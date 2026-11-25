// пакет dns — неблокирующийся резолвер a-записей поверх одного udp-сокета.
// DNS-пакеты формирует и разбирает библиотека dnsmessage.
package dns

import (
	"bufio"
	"container/list"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"os"

	"strings"
	"syscall"
	"time"
)

const (
	queryTimeout = 5 * time.Second // время ожидания ответа перед повторной попыткой
	maxAttempts  = 3
	maxPending   = 4096 // предел одновременных запросов
	maxPacket    = 1500 //размер буфера для приёма DNS-датаграммы
)

// ошибки резолвинга
var (
	ErrTimeout  = errors.New("dns: таймаут")
	ErrNotFound = errors.New("dns: адрес не найден")
	ErrServFail = errors.New("dns: ошибка сервера")
	ErrBadName  = errors.New("dns: некорректное имя")
	ErrFormat   = errors.New("dns: некорректный ответ")
	ErrBusy     = errors.New("dns: слишком много запросов")
)

// Callback вызывается один раз: с адресом или с ошибкой
type Callback func(ip [4]byte, err error)

// Query — ожидающий ответа запрос
type Query struct {
	name     string
	id       uint16
	deadline time.Time //время следующей проверки
	cb       Callback
	timer    *list.Element
	packet   []byte //сохраняем готовый dns пакет чтобы повторить отправку если надо
	attempts int
}

// Resolver держит udp-сокет и очередь запросов
type Resolver struct {
	fd      int
	server  syscall.SockaddrInet4 //адрес dns сервака
	pending map[uint16]*Query     //словарь запросов
	queue   list.List             //очередь запросов, в порядке дедлайнов
	buf     []byte
}

// New создаёт неблокирующийся UDP-сокет для указанного адреса и порта DNS
func New(server syscall.SockaddrInet4) (*Resolver, error) {
	if server.Port < 1 || server.Port > 65535 {
		return nil, errors.New("dns: некорректный порт")
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	//семейство адресов(4 или 6),данные пакетами,неблкирующий режим,выбрать стандартный протокол на основе типа сокета(udp)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fd:      fd,
		server:  server,
		pending: make(map[uint16]*Query),
		buf:     make([]byte, maxPacket),
	}, nil
}

// SystemServer читает первый ipv4 nameserver из /etc/resolv.conf
func SystemServer() ([4]byte, error) {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return [4]byte{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if ip := net.ParseIP(fields[1]).To4(); ip != nil {
				return [4]byte(ip), nil
			}
		}
	}
	return [4]byte{}, errors.New("в /etc/resolv.conf нет ipv4 nameserver")
}

// Fd возвращает дескриптор сокета для регистрации в селекторе.
func (r *Resolver) Fd() int { return r.fd }

// Query отправляет запрос a-записи. ответ придёт в cb из HandleReadable или Expire.
func (r *Resolver) Query(name string, cb Callback) (*Query, error) {
	if len(r.pending) >= maxPending {
		return nil, ErrBusy
	}
	var id uint16 //генерим уникальный id
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil { //Заполняем массив случайными байтами
			return nil, err
		}
		id = binary.BigEndian.Uint16(b[:])
		if _, used := r.pending[id]; !used {
			break
		}
	}
	msg, err := buildQuery(id, name)
	if err != nil {
		return nil, err
	}
	q := &Query{name: strings.TrimSuffix(name, ".") + ".", id: id, packet: msg, cb: cb}
	if err := r.attempt(q, time.Now()); err != nil {
		return nil, err
	}
	r.pending[id] = q
	q.timer = r.queue.PushBack(q)
	return q, nil
}

// attempt отправляет одну датаграмму; ожидание ответа выполняет селектор.
func (r *Resolver) attempt(q *Query, now time.Time) error {
	q.attempts++
	q.deadline = now.Add(queryTimeout)
	err := syscall.Sendto(r.fd, q.packet, 0, &r.server)
	//0 - флаги отправки
	// Временная невозможность отправки тоже расходует попытку
	if err == syscall.EAGAIN || err == syscall.EINTR || err == syscall.ENOBUFS {
		return nil
	}
	return err
}

// Cancel отменяет запрос, колбэк не будет вызван
func (r *Resolver) Cancel(q *Query) {
	if q == nil || q.timer == nil || r.pending[q.id] != q {
		return
	}
	r.queue.Remove(q.timer)
	delete(r.pending, q.id)
	q.timer = nil
	q.cb = nil
	q.packet = nil
}

// HandleReadable читает одну датаграмму и завершает соответствующий запрос.
func (r *Resolver) HandleReadable() {
	n, from, err := syscall.Recvfrom(r.fd, r.buf, 0)
	if err != nil {
		return // eagain и прочее: ждём следующего события
	}
	sa, ok := from.(*syscall.SockaddrInet4)
	if !ok || sa.Addr != r.server.Addr || sa.Port != r.server.Port {
		return // ответ не от нашего резолвера
	}
	var m dnsmessage.Message
	if err := m.Unpack(r.buf[:n]); err != nil {
		return
	}
	q := r.pending[m.ID]
	if q == nil || !m.Response || len(m.Questions) != 1 {
		return
	}
	question := m.Questions[0]
	if !strings.EqualFold(question.Name.String(), q.name) ||
		question.Type != dnsmessage.TypeA || question.Class != dnsmessage.ClassINET {
		return //наш ли это вопрос
	}
	ip, err := responseIP(&m)
	r.complete(q, ip, err) //вызов callback
}

// Expire повторяет запрос по таймеру; после третьей попытки сообщает тайм-аут.
func (r *Resolver) Expire(now time.Time) {
	for front := r.queue.Front(); front != nil; front = r.queue.Front() {
		q := front.Value.(*Query)
		if now.Before(q.deadline) {
			return
		}
		if q.attempts >= maxAttempts {
			r.complete(q, [4]byte{}, ErrTimeout)
		} else if err := r.attempt(q, now); err != nil {
			r.complete(q, [4]byte{}, err)
		} else {
			r.queue.MoveToBack(front)
		}
	}
}

// NextDeadline возвращает ближайший дедлайн, если есть ожидающие запросы.
func (r *Resolver) NextDeadline() (time.Time, bool) {
	front := r.queue.Front()
	if front == nil {
		return time.Time{}, false
	}
	return front.Value.(*Query).deadline, true
}

func (r *Resolver) complete(q *Query, ip [4]byte, err error) {
	cb := q.cb
	r.Cancel(q)
	cb(ip, err)
}

// buildQuery формирует A-запрос с помощью библиотеки
func buildQuery(id uint16, name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return nil, ErrBadName
	}
	n, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return nil, ErrBadName
	}
	m := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	packet, err := m.Pack()
	if err != nil {
		return nil, ErrBadName
	}
	return packet, nil
}

// извлечение адреса
func parseResponse(packet []byte) (uint16, [4]byte, error) {
	var m dnsmessage.Message
	if err := m.Unpack(packet); err != nil {
		return 0, [4]byte{}, ErrFormat
	}
	ip, err := responseIP(&m)
	return m.ID, ip, err
}

func responseIP(m *dnsmessage.Message) ([4]byte, error) {
	if !m.Response || m.Truncated {
		return [4]byte{}, ErrFormat
	}
	switch m.RCode {
	case dnsmessage.RCodeSuccess:
	case dnsmessage.RCodeNameError:
		return [4]byte{}, ErrNotFound
	default:
		return [4]byte{}, ErrServFail
	}
	if len(m.Questions) != 1 {
		return [4]byte{}, ErrFormat
	}
	name := strings.ToLower(m.Questions[0].Name.String())
	addresses := make(map[string][4]byte)
	aliases := make(map[string]string)
	for _, answer := range m.Answers {
		if answer.Header.Class != dnsmessage.ClassINET {
			continue
		}
		owner := strings.ToLower(answer.Header.Name.String())
		switch body := answer.Body.(type) {
		case *dnsmessage.AResource:
			if _, exists := addresses[owner]; !exists {
				addresses[owner] = body.A
			}
		case *dnsmessage.CNAMEResource:
			aliases[owner] = strings.ToLower(body.CNAME.String())
		}
	}
	// Длина цепочки ограничена числом CNAME в ответе, в том числе при цикле.
	for i := 0; i <= len(aliases); i++ {
		next, ok := aliases[name]
		if !ok {
			if ip, found := addresses[name]; found {
				return ip, nil
			}
			return [4]byte{}, ErrNotFound
		}
		name = next
	}
	return [4]byte{}, ErrFormat
}
