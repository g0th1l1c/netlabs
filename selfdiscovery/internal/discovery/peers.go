package discovery

import (
	"sort"
	"sync"
	"time"
)

// PeerTable хранит идентификаторы живых копий приложения и момент,когда от каждой из них последний раз приходило сообщение
type PeerTable struct {
	mu    sync.Mutex
	peers map[string]time.Time // ключ — selfID копии
}

func NewPeerTable() *PeerTable {
	return &PeerTable{peers: make(map[string]time.Time)}
}

// Touch отмечает, что от копии с данным id только что пришло сообщение
func (t *PeerTable) Touch(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, existed := t.peers[id]
	t.peers[id] = time.Now()
	return !existed
}

// RemoveExpired удаляет копии, от которых давно не было сообщений
func (t *PeerTable) RemoveExpired(timeout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	changed := false
	deadline := time.Now().Add(-timeout)
	for id, last := range t.peers {
		if last.Before(deadline) {
			delete(t.peers, id)
			changed = true
		}
	}
	return changed
}

// List возвращает отсортированный снимок текущего списка живых ID
func (t *PeerTable) List() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	list := make([]string, 0, len(t.peers))
	for id := range t.peers {
		list = append(list, id)
	}
	sort.Strings(list)
	return list
}