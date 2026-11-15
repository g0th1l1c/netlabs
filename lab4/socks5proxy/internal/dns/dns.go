// пакет dns — неблокирующийся резолвер a-записей поверх одного udp-сокета.
// DNS-пакеты формирует и разбирает библиотека dnsmessage.
package dns

import (
	"bufio"
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
	queryTimeout = 5 * time.Second // время ожидания ответа
	maxPending   = 4096            // предел одновременных запросов
	maxPacket    = 1500
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

// Callback вызывается один раз: с адресом или с ошибкой.
type Callback func(ip [4]byte, err error)

// Query — ожидающий ответа запрос.
type Query struct {
	name     string
	id       uint16
	deadline time.Time
	cb       Callback
	done     bool
}

// Resolver держит udp-сокет и очередь запросов.
type Resolver struct {
	fd      int
	server  syscall.SockaddrInet4
	pending map[uint16]*Query
	queue   []*Query // в порядке дедлайнов (таймаут у всех одинаковый)
	buf     []byte
}

// New создаёт неблокирующийся udp-сокет для резолвера server:53.
func New(server [4]byte) (*Resolver, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fd:      fd,
		server:  syscall.SockaddrInet4{Port: 53, Addr: server},
		pending: make(map[uint16]*Query),
		buf:     make([]byte, maxPacket),
	}, nil
}

// SystemServer читает первый ipv4 nameserver из /etc/resolv.conf.
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
	var id uint16
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
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
	if err := syscall.Sendto(r.fd, msg, 0, &r.server); err != nil {
		return nil, err
	}
	q := &Query{name: strings.TrimSuffix(name, ".") + ".", id: id, deadline: time.Now().Add(queryTimeout), cb: cb}
	r.pending[id] = q
	r.queue = append(r.queue, q)
	return q, nil
}

// Cancel отменяет запрос, колбэк не будет вызван.
func (r *Resolver) Cancel(q *Query) {
	if q != nil && !q.done {
		q.done = true
		delete(r.pending, q.id)
	}
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
		return
	}
	ip, err := responseIP(&m)
	r.complete(q, ip, err)
}

// Expire завершает ошибкой запросы с истёкшим дедлайном.
func (r *Resolver) Expire(now time.Time) {
	for len(r.queue) > 0 {
		q := r.queue[0]
		if !q.done && now.Before(q.deadline) {
			return
		}
		r.queue[0] = nil
		r.queue = r.queue[1:]
		if !q.done {
			r.complete(q, [4]byte{}, ErrTimeout)
		}
	}
}

// NextDeadline возвращает ближайший дедлайн, если есть ожидающие запросы.
func (r *Resolver) NextDeadline() (time.Time, bool) {
	for len(r.queue) > 0 && r.queue[0].done {
		r.queue[0] = nil
		r.queue = r.queue[1:]
	}
	if len(r.queue) == 0 {
		return time.Time{}, false
	}
	return r.queue[0].deadline, true
}

func (r *Resolver) complete(q *Query, ip [4]byte, err error) {
	q.done = true
	delete(r.pending, q.id)
	q.cb(ip, err)
}

// buildQuery формирует A-запрос с помощью библиотеки.
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
	for _, answer := range m.Answers {
		if a, ok := answer.Body.(*dnsmessage.AResource); ok && answer.Header.Class == dnsmessage.ClassINET {
			return a.A, nil
		}
	}
	return [4]byte{}, ErrNotFound
}
