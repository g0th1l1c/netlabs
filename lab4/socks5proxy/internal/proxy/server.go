// пакет proxy — ядро socks5-прокси: цикл событий и конечные автоматы сессий.
// всё работает в одном потоке на неблокирующихся сокетах.
package proxy

import (
	"fmt"

	"syscall"
	"time"

	"socks5proxy/internal/dns"
	"socks5proxy/internal/poller"
)



// owner связывает дескриптор сокета с сессией и стороной (клиент или цель)
type owner struct {
	s      *session
	target bool 
	//false- наш клиент ,true-сайт
}

// Server — слушающий сокет, селектор, резолвер и таблица дескрипторов
type Server struct {
	poll    *poller.Poller
	lfd     int //сокет принимающий соединения
	dns     *dns.Resolver
	fds     map[int]owner //словарик подключений(дескриптор - владелец)
	paused  bool // приём подключений приостановлен из-за нехватки ресурсов
	retryAt time.Time
}

// New создаёт слушающий сокет на порту и резолвер с адресом dnsServer.
func New(port int, dnsServer syscall.SockaddrInet4) (*Server, error) {
	p, err := poller.New(256)
	if err != nil {
		return nil, err
	}
	lfd, err := listen(port)
	if err != nil {
		return nil, err
	}
	r, err := dns.New(dnsServer)
	if err != nil {
		return nil, err
	}
	if err := p.Add(lfd, poller.In); err != nil {
		return nil, err
	}
	if err := p.Add(r.Fd(), poller.In); err != nil {
		return nil, err
	}
	return &Server{poll: p, lfd: lfd, dns: r, fds: make(map[int]owner)}, nil
}

func listen(port int) (int, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	//настраиваем сокет
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return -1, err     			  //настройки на уровне сокета,разрешить повторное использование адреса(после закрытия TCP-соединений система некоторое время может хранить информацию о них)
	}
	//привязка сокета к порту
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: port}); err != nil {
		return -1, err
	}
	//режим прослушивания
	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		return -1, err
	}
	return fd, nil
}

// Run — главный цикл, единственный блокирующий вызов — ожидание селектора
func (s *Server) Run() error {
	events := make([]poller.Event, 256) //массив под epoll
	for {
		n, err := s.poll.Wait(s.timeout(), events)
		if err != nil {
			return err
		}
		if err := s.processEvents(events[:n]); err != nil {
			return err
		}
	}
}

// timeout учитывает DNS-дедлайны и повторную попытку приёма клиентов.
func (s *Server) timeout() int {
	d, ok := s.dns.NextDeadline()
	if s.paused && (!ok || s.retryAt.Before(d)) {
		d, ok = s.retryAt, true
	}
	if !ok { //ждем каких либо событий
		return -1
	}
	ms := time.Until(d).Milliseconds() + 1
	if ms < 0 {
		ms = 0
	}
	return int(ms)
}

// processEvents сохраняет владельцев до обработки пачки: закрытый fd может быть переиспользован accept или connect уже в этой же пачке.
//распределяем события
func (s *Server) processEvents(events []poller.Event) error {
	var owners [256]owner
	for i, e := range events {
		owners[i] = s.fds[e.Fd]
	}
	now := time.Now()
	s.dns.Expire(now) //проверяем просроченный dns запросы
	if s.paused && !now.Before(s.retryAt) {
		//попробуем снова принимать подключения
		s.resumeAccept()
	}
	for i, e := range events {
		switch e.Fd {
		case s.lfd:
			if !s.paused {
				if err := s.accept(); err != nil {
					return err
				}
			}
		case s.dns.Fd():
			s.dns.HandleReadable() //Передаём управление резолверу
		default:
			o := owners[i]
			if o.s != nil && !o.s.closed && s.fds[e.Fd] == o {
				o.s.onEvent(o.target, e.Events)
			}
		}
	}
	return nil
}

// accept принимает одно подключение 
func (s *Server) accept() error {
	fd, _, err := syscall.Accept4(s.lfd, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
	switch err {
	case nil:
		newSession(s, fd)
	case syscall.EAGAIN, syscall.EINTR, syscall.ECONNABORTED, 
		syscall.ENETDOWN, syscall.EPROTO, syscall.ENOPROTOOPT,
		syscall.EHOSTDOWN, syscall.ENONET, syscall.EHOSTUNREACH,
		syscall.EOPNOTSUPP, syscall.ENETUNREACH:
		//Просто возвращаемся к ожиданию
		return nil
	case syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS, syscall.ENOMEM:
		//приостанавливаем приём
		return s.pauseAccept()
	default:
		return fmt.Errorf("accept: %w", err)
	}
	return nil
}

const acceptRetryDelay = time.Second //повторная попытка через сек

func (s *Server) pauseAccept() error {
	if err := s.poll.Del(s.lfd); err != nil {
		return err
	}
	s.paused = true
	s.retryAt = time.Now().Add(acceptRetryDelay)
	return nil
}

func (s *Server) resumeAccept() {
	if err := s.poll.Add(s.lfd, poller.In); err == nil {
		s.paused = false
	} else {
		s.retryAt = time.Now().Add(acceptRetryDelay)
	}
}

// Освобождение ресурсов позволяет попробовать раньше таймера
func (s *Server) sessionClosed() {
	if s.paused {
		s.resumeAccept()
	}
}
