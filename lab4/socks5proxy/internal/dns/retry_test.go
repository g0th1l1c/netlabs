package dns

import (
	"bytes"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"socks5proxy/internal/poller"
	"syscall"
	"testing"
	"time"
)

func TestRetries(t *testing.T) {
	for _, mode := range []string{"timeout", "response", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			r, err := New(syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}, Port: peer.LocalAddr().(*net.UDPAddr).Port})
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(r.fd)
			called := 0
			var result error
			q, err := r.Query("example.test", func(ip [4]byte, err error) {
				called++
				result = err
				if mode == "response" && ip != [4]byte{1, 2, 3, 4} {
					t.Errorf("ip=%v", ip)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			receive := func() ([]byte, *net.UDPAddr) {
				t.Helper()
				peer.SetReadDeadline(time.Now().Add(time.Second))
				b := make([]byte, 1500)
				n, from, err := peer.ReadFromUDP(b)
				if err != nil {
					t.Fatal(err)
				}
				return b[:n], from
			}
			first, _ := receive()
			r.Expire(q.deadline.Add(-time.Nanosecond))
			if q.attempts != 1 || called != 0 {
				t.Fatal("early retry")
			}
			r.Expire(q.deadline)
			second, from := receive()
			if !bytes.Equal(first, second) || q.attempts != 2 || called != 0 {
				t.Fatal("second attempt")
			}
			if d, ok := r.NextDeadline(); !ok || !d.Equal(q.deadline) {
				t.Fatal("missing retry deadline")
			}
			switch mode {
			case "timeout":
				r.Expire(q.deadline)
				third, _ := receive()
				if !bytes.Equal(first, third) || q.attempts != 3 || called != 0 {
					t.Fatal("third attempt")
				}
				r.Expire(q.deadline)
				if called != 1 || result != ErrTimeout {
					t.Fatalf("callback=%d err=%v", called, result)
				}
			case "response":
				var m dnsmessage.Message
				if err := m.Unpack(second); err != nil {
					t.Fatal(err)
				}
				m.Response = true
				m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{1, 2, 3, 4}}}}
				b, err := m.Pack()
				if err != nil {
					t.Fatal(err)
				}
				p, err := poller.New(1)
				if err != nil {
					t.Fatal(err)
				}
				// Poller has no public Close in the original Linux lab.
				if err = p.Add(r.fd, poller.In); err != nil {
					t.Fatal(err)
				}
				if _, err = peer.WriteToUDP(b, from); err != nil {
					t.Fatal(err)
				}
				if n, err := p.Wait(1000, make([]poller.Event, 1)); err != nil || n != 1 {
					t.Fatal("response readiness", err)
				}
				r.HandleReadable()
				if called != 1 || result != nil {
					t.Fatalf("callback=%d err=%v", called, result)
				}
			case "cancel":
				r.Cancel(q)
			}
			r.Expire(q.deadline.Add(10 * queryTimeout))
			if _, ok := r.NextDeadline(); ok || len(r.pending) != 0 {
				t.Fatal("request not removed")
			}
			if mode == "cancel" && called != 0 || mode != "cancel" && called != 1 {
				t.Fatal("extra callback")
			}
			peer.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			if _, _, err := peer.ReadFromUDP(make([]byte, 1500)); err == nil {
				t.Fatal("unexpected extra DNS attempt")
			}
		})
	}
}

func TestFinishedQueriesRemovedBeforeEarlierDeadline(t *testing.T) {
	r := &Resolver{pending: make(map[uint16]*Query)}
	earlier := &Query{id: 1, deadline: time.Now().Add(queryTimeout)}
	r.pending[earlier.id] = earlier
	earlier.timer = r.queue.PushBack(earlier)

	for _, cancel := range []bool{true, false} {
		called := false
		q := &Query{id: 2, packet: []byte{1}, cb: func([4]byte, error) { called = true }}
		r.pending[q.id] = q
		q.timer = r.queue.PushBack(q)
		if cancel {
			r.Cancel(q)
		} else {
			r.complete(q, [4]byte{}, ErrNotFound)
		}
		if called == cancel || q.cb != nil || q.packet != nil || q.timer != nil || r.queue.Len() != 1 || len(r.pending) != 1 {
			t.Fatal("finished query retained or callback handled incorrectly")
		}
	}
}
