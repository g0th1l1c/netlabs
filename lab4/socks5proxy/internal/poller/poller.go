//go:build linux

// пакет poller — тонкая обёртка над epoll (селектор).
// режим level-triggered: событие повторяется, пока условие не снято.
package poller

import "syscall"

// маски событий
const (
	In  uint32 = syscall.EPOLLIN
	Out uint32 = syscall.EPOLLOUT
	Err uint32 = syscall.EPOLLERR
	Hup uint32 = syscall.EPOLLHUP
)

// Event — готовое событие дескриптора.
type Event struct {
	Fd     int
	Events uint32
}

// Poller — экземпляр epoll.
type Poller struct {
	fd  int
	raw []syscall.EpollEvent
}

// New создаёт epoll, maxEvents — размер пачки событий за один Wait.
func New(maxEvents int) (*Poller, error) {
	fd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &Poller{fd: fd, raw: make([]syscall.EpollEvent, maxEvents)}, nil
}

func (p *Poller) ctl(op, fd int, events uint32) error {
	ev := syscall.EpollEvent{Events: events, Fd: int32(fd)}
	return syscall.EpollCtl(p.fd, op, fd, &ev)
}

// Add регистрирует дескриптор.
func (p *Poller) Add(fd int, events uint32) error { return p.ctl(syscall.EPOLL_CTL_ADD, fd, events) }

// Mod меняет набор отслеживаемых событий.
func (p *Poller) Mod(fd int, events uint32) error { return p.ctl(syscall.EPOLL_CTL_MOD, fd, events) }

// Del снимает дескриптор с отслеживания.
func (p *Poller) Del(fd int) error { return p.ctl(syscall.EPOLL_CTL_DEL, fd, 0) }

// Wait ждёт события до timeoutMs миллисекунд (-1 — бесконечно)
// и заполняет out. возвращает число событий.
func (p *Poller) Wait(timeoutMs int, out []Event) (int, error) {
	limit := len(out)
	if limit > len(p.raw) {
		limit = len(p.raw)
	}
	n, err := syscall.EpollWait(p.fd, p.raw[:limit], timeoutMs)
	if err != nil {
		if err == syscall.EINTR {
			return 0, nil
		}
		return 0, err
	}
	for i := 0; i < n; i++ {
		out[i] = Event{Fd: int(p.raw[i].Fd), Events: p.raw[i].Events}
	}
	return n, nil
}
