package proxy

import (
	"syscall"
	"testing"
	"time"

	"socks5proxy/internal/poller"
)

func reviewConnect(t *testing.T, lfd int) int {
	t.Helper()
	sa, err := syscall.Getsockname(lfd)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	addr := sa.(*syscall.SockaddrInet4)
	addr.Addr = [4]byte{127, 0, 0, 1}
	if err = syscall.Connect(fd, addr); err != nil {
		t.Fatal(err)
	}
	return fd
}

func reviewResetAndFill(t *testing.T, fd int) {
	t.Helper()
	if err := syscall.SetsockoptLinger(fd, syscall.SOL_SOCKET, syscall.SO_LINGER, &syscall.Linger{Onoff: 1, Linger: 0}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatal(err)
	}
	replacement, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil || replacement != fd {
		t.Fatalf("fill freed peer fd %d: got %d, %v", fd, replacement, err)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestReviewEventBatchDescriptorReuse(t *testing.T) {
	srv, err := New(0, [4]byte{127, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	clientPeer := reviewConnect(t, srv.lfd)
	srv.accept()
	var old *session
	for _, o := range srv.fds {
		old = o.s
	}
	if old == nil {
		t.Fatal("no accepted client")
	}
	targetListener, err := listen(0)
	if err != nil {
		t.Fatal(err)
	}
	targetPeer := reviewConnect(t, targetListener)
	targetFD, _, err := syscall.Accept4(targetListener, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	old.tfd = targetFD
	old.state = stRelay
	srv.fds[targetFD] = owner{s: old, target: true}
	old.update()
	oldClientFD := old.cfd
	initial := make([]poller.Event, 256)
	if n, err := srv.poll.Wait(0, initial); err != nil || n != 0 {
		t.Fatalf("unexpected setup events: %+v, %v", initial[:n], err)
	}

	// Establish real epoll readiness in a deliberate FIFO order.
	reviewResetAndFill(t, targetPeer)
	newClientPeer := reviewConnect(t, srv.lfd)
	defer syscall.Close(newClientPeer)
	time.Sleep(10 * time.Millisecond)
	reviewResetAndFill(t, clientPeer)

	events := make([]poller.Event, 256)
	n, err := srv.poll.Wait(1000, events)
	if err != nil {
		t.Fatal(err)
	}
	events = events[:n]
	t.Logf("old client fd=%d target fd=%d listener=%d; actual epoll events=%+v", oldClientFD, targetFD, srv.lfd, events)
	targetIndex, listenerIndex, clientIndex := -1, -1, -1
	for i, e := range events {
		switch e.Fd {
		case targetFD:
			targetIndex = i
		case srv.lfd:
			listenerIndex = i
		case oldClientFD:
			clientIndex = i
		}
	}
	if !(targetIndex >= 0 && targetIndex < listenerIndex && listenerIndex < clientIndex) {
		t.Fatal("unexpected actual readiness order")
	}

	if err := srv.processEvents(events); err != nil {
		t.Fatal(err)
	}
	replacement := srv.fds[oldClientFD].s
	if replacement == nil || replacement == old || replacement.closed {
		t.Fatal("stale event closed the replacement connection")
	}
	replacement.close()
}

func TestAcceptResumesWithoutSessions(t *testing.T) {
	srv, err := New(0, [4]byte{127, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(srv.lfd)
	defer syscall.Close(srv.dns.Fd())
	if err := srv.pauseAccept(); err != nil {
		t.Fatal(err)
	}
	if ms := srv.timeout(); ms <= 0 || ms > 1001 {
		t.Fatalf("retry must wait in selector, timeout=%d", ms)
	}
	peer := reviewConnect(t, srv.lfd)
	defer syscall.Close(peer)
	srv.retryAt = time.Now().Add(-time.Millisecond)
	if err := srv.processEvents(nil); err != nil {
		t.Fatal(err)
	}
	if srv.paused {
		t.Fatal("listener did not resume without sessions")
	}
	events := make([]poller.Event, 256)
	n, err := srv.poll.Wait(1000, events)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.processEvents(events[:n]); err != nil {
		t.Fatal(err)
	}
	if len(srv.fds) != 1 {
		t.Fatal("pending client was not accepted")
	}
	for _, o := range srv.fds {
		o.s.close()
	}
}
