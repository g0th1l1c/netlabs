package proxy

import (
	"bytes"
	"encoding/binary"

	"syscall"

	"socks5proxy/internal/dns"
	"socks5proxy/internal/poller"
)

type state int

const (
	stGreeting   state = iota // ждём список методов аутентификации
	stRequest                 // ждём запрос connect
	stResolving               // ждём dns-ответ
	stConnecting              // ждём завершения connect к цели
	stRelay                   // пересылаем данные в обе стороны
)

// session — одно клиентское подключение и (после запроса) соединение с целью.
type session struct {
	srv    *Server
	cfd    int // сокет клиента
	tfd    int // сокет цели, -1 пока нет
	state  state
	query  *dns.Query
	port   uint16 // порт цели для dns-случая
	c2t    buffer // клиент -> цель (на этапе рукопожатия: вход от клиента)
	t2c    buffer // цель -> клиент (на этапе рукопожатия: ответы клиенту)
	cEOF   bool   // клиент закрыл свою запись
	tEOF   bool   // цель закрыла свою запись
	cShut  bool   // мы закрыли запись клиенту
	tShut  bool   // мы закрыли запись цели
	cMask  uint32 // события клиента в селекторе, 0 — не зарегистрирован
	tMask  uint32
	failed bool // закрыть после отправки накопленного ответа
	closed bool
}

func newSession(srv *Server, cfd int) {
	s := &session{srv: srv, cfd: cfd, tfd: -1, c2t: newBuffer(), t2c: newBuffer()}
	srv.fds[cfd] = owner{s: s}
	syscall.SetsockoptInt(cfd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
	s.update()
}

// onEvent обрабатывает событие одного из сокетов сессии.
func (s *session) onEvent(target bool, ev uint32) {
	if target {
		s.onTarget(ev)
	} else {
		s.onClient(ev)
	}
	s.update()
}

func (s *session) onClient(ev uint32) {
	if ev&poller.Err != 0 {
		s.close()
		return
	}
	if ev&poller.In != 0 {
		s.readClient()
	}
	if !s.closed && ev&poller.Out != 0 && !send(s.cfd, &s.t2c) {
		s.close()
		return
	}
	if !s.closed && ev&poller.Hup != 0 && ev&poller.In == 0 {
		s.close()
	}
}

func (s *session) onTarget(ev uint32) {
	if s.state == stConnecting {
		if ev&(poller.Out|poller.Err|poller.Hup) != 0 {
			s.finishConnect()
		}
		return
	}
	if ev&poller.Err != 0 {
		s.close()
		return
	}
	if ev&poller.In != 0 {
		eof, ok := recv(s.tfd, &s.t2c)
		if !ok {
			s.close()
			return
		}
		s.tEOF = s.tEOF || eof
	}
	if !s.closed && ev&poller.Out != 0 && !send(s.tfd, &s.c2t) {
		s.close()
		return
	}
	if !s.closed && ev&poller.Hup != 0 && ev&poller.In == 0 {
		s.close()
	}
}

func (s *session) readClient() {
	eof, ok := recv(s.cfd, &s.c2t)
	if !ok {
		s.close()
		return
	}
	if eof {
		s.cEOF = true
		if s.state <= stRequest {
			s.close() // клиент ушёл до запроса
			return
		}
	}
	if s.state <= stRequest {
		s.handshake()
	}
}

// handshake разбирает накопленные байты рукопожатия.
// лишние байты после запроса остаются в c2t как данные для цели.
func (s *session) handshake() {
	for !s.closed && !s.failed {
		data := s.c2t.readable()
		switch s.state {
		case stGreeting:
			if len(data) < 2 {
				return
			}
			if data[0] != ver5 {
				s.close()
				return
			}
			n := int(data[1])
			if len(data) < 2+n {
				return
			}
			hasNoAuth := bytes.IndexByte(data[2:2+n], methodNoAuth) >= 0
			s.c2t.r += 2 + n
			if !hasNoAuth {
				s.t2c.put([]byte{ver5, methodNoAccepted})
				s.failed = true
				return
			}
			s.t2c.put([]byte{ver5, methodNoAuth})
			s.state = stRequest
		case stRequest:
			s.parseRequest(data)
			return
		default:
			return
		}
	}
}

func (s *session) parseRequest(data []byte) {
	if len(data) < 4 {
		return
	}
	if data[0] != ver5 {
		s.close()
		return
	}
	if data[1] != cmdConnect {
		s.fail(repCmdNotSupported)
		return
	}
	var need int
	switch data[3] {
	case atypIPv4:
		need = 4 + 4 + 2
	case atypDomain:
		if len(data) < 5 {
			return
		}
		need = 4 + 1 + int(data[4]) + 2
	default:
		s.fail(repAtypNotSupported)
		return
	}
	if len(data) < need {
		return
	}
	port := binary.BigEndian.Uint16(data[need-2:])

	if data[3] == atypIPv4 {
		addr := [4]byte(data[4:8])
		s.c2t.r += need
		s.connect(addr, port)
		return
	}
	name := string(data[5 : need-2])
	s.c2t.r += need
	s.resolve(name, port)
}

func (s *session) resolve(name string, port uint16) {
	s.state = stResolving
	s.port = port
	q, err := s.srv.dns.Query(name, s.onResolved)
	if err != nil {
		s.fail(repHostUnreachable)
		return
	}
	s.query = q
}

// onResolved вызывается резолвером; не из обработчика событий сессии.
func (s *session) onResolved(ip [4]byte, err error) {
	s.query = nil
	if s.closed {
		return
	}
	if err != nil {
		s.fail(repHostUnreachable)
	} else {
		s.connect(ip, s.port)
	}
	s.update()
}

// connect начинает неблокирующее подключение к цели.
func (s *session) connect(addr [4]byte, port uint16) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		s.fail(repFailure)
		return
	}
	s.tfd = fd
	s.srv.fds[fd] = owner{s: s, target: true}
	syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

	err = syscall.Connect(fd, &syscall.SockaddrInet4{Port: int(port), Addr: addr})
	switch err {
	case nil:
		s.connected()
	case syscall.EINPROGRESS, syscall.EINTR:
		s.state = stConnecting
	default:
		s.dropTarget()
		s.fail(repFromErrno(err))
	}
}

func (s *session) finishConnect() {
	v, err := syscall.GetsockoptInt(s.tfd, syscall.SOL_SOCKET, syscall.SO_ERROR)
	if err != nil {
		v = int(syscall.EIO)
	}
	if v != 0 {
		s.dropTarget()
		s.fail(repFromErrno(syscall.Errno(v)))
		return
	}
	s.connected()
}

// connected отправляет клиенту успех и переключает сессию в режим пересылки.
func (s *session) connected() {
	var addr [4]byte
	var port uint16
	if sa, err := syscall.Getsockname(s.tfd); err == nil {
		if in4, ok := sa.(*syscall.SockaddrInet4); ok {
			addr, port = in4.Addr, uint16(in4.Port)
		}
	}
	s.t2c.put(reply(repOK, addr, port))
	s.state = stRelay
}

// fail ставит в очередь ответ с ошибкой и закроет сессию после его отправки.
func (s *session) fail(code byte) {
	s.t2c.put(reply(code, [4]byte{}, 0))
	s.failed = true
}

// dropTarget закрывает сокет цели, сессия остаётся жить.
func (s *session) dropTarget() {
	if s.tfd < 0 {
		return
	}
	delete(s.srv.fds, s.tfd)
	syscall.Close(s.tfd)
	s.tfd, s.tMask = -1, 0
}

// update аккуратно закрывает половины соединений и пересчитывает
// набор событий каждого сокета: ждём только то, что реально можно сделать.
// благодаря этому селектор никогда не будит программу вхолостую.
func (s *session) update() {
	if s.closed {
		return
	}
	if s.state == stRelay {
		if s.cEOF && s.c2t.empty() && !s.tShut {
			syscall.Shutdown(s.tfd, syscall.SHUT_WR)
			s.tShut = true
		}
		if s.tEOF && s.t2c.empty() && !s.cShut {
			syscall.Shutdown(s.cfd, syscall.SHUT_WR)
			s.cShut = true
		}
		if s.tShut && s.cShut {
			s.close()
			return
		}
	}
	if s.failed && s.t2c.empty() {
		s.close()
		return
	}

	var cm, tm uint32
	if !s.cEOF && !s.failed && s.c2t.hasSpace() {
		cm |= poller.In
	}
	if !s.t2c.empty() {
		cm |= poller.Out
	}
	if s.tfd >= 0 {
		switch s.state {
		case stConnecting:
			tm = poller.Out
		case stRelay:
			if !s.tEOF && s.t2c.hasSpace() {
				tm |= poller.In
			}
			if !s.c2t.empty() {
				tm |= poller.Out
			}
		}
	}
	if !s.setMask(false, cm) || !s.setMask(true, tm) {
		s.close()
	}
}

// setMask приводит регистрацию сокета в селекторе к нужной маске.
func (s *session) setMask(target bool, want uint32) bool {
	fd, cur := s.cfd, &s.cMask
	if target {
		fd, cur = s.tfd, &s.tMask
	}
	if fd < 0 || want == *cur {
		return true
	}
	var err error
	switch {
	case *cur == 0:
		err = s.srv.poll.Add(fd, want)
	case want == 0:
		err = s.srv.poll.Del(fd)
	default:
		err = s.srv.poll.Mod(fd, want)
	}
	if err != nil {
		return false
	}
	*cur = want
	return true
}

// close освобождает все ресурсы сессии.
func (s *session) close() {
	if s.closed {
		return
	}
	s.closed = true
	s.srv.dns.Cancel(s.query)
	s.query = nil
	delete(s.srv.fds, s.cfd)
	syscall.Close(s.cfd)
	s.dropTarget()
	s.srv.sessionClosed()
}
