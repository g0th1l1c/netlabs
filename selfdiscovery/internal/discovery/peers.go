package discovery

import (
	"sort"
	"sync"
	"time"
)

// хранит адреса "живых" копий приложения и момент, когда от каждой из них последний раз приходило сообщение
type PeerTable struct {
	mu    sync.Mutex
	peers map[string]time.Time
}

func NewPeerTable() *PeerTable {
	return &PeerTable{peers: make(map[string]time.Time)}
}

//отмечает, что от адреса ip только что пришло сообщение
func (t *PeerTable) Touch(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, existed := t.peers[ip]
	t.peers[ip] = time.Now()
	return !existed
}

//  удаляет адреса, от которых давно не было сообщений
func (t *PeerTable) RemoveExpired(timeout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	changed := false
	deadline := time.Now().Add(-timeout)
	for ip, last := range t.peers {
		if last.Before(deadline) {
			delete(t.peers, ip)
			changed = true
		}
	}
	return changed
}

//возвращает отсортированный снимок текущего списка живых адресов
func (t *PeerTable) List() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	list := make([]string, 0, len(t.peers))
	for ip := range t.peers {
		list = append(list, ip)
	}
	sort.Strings(list)
	return list
}
