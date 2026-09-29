package discovery

import (
	"sort"
	"sync"
	"time"
)

//информация об одной живой копии приложения
type Peer struct {
	ID string
	IP string
}

//внутренняя запись: последний известный IP и время последнего сообщения
type entry struct {
	ip       string
	lastSeen time.Time
}

//хранит "живые" копии приложения
type PeerTable struct {
	mu    sync.Mutex
	peers map[string]entry
}

func NewPeerTable() *PeerTable {
	return &PeerTable{peers: make(map[string]entry)}
}

//отмечает, что от копии id с адреса ip только что пришло сообщение
func (t *PeerTable) Touch(id, ip string) (changed bool, oldIP string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	prev, existed := t.peers[id]
	t.peers[id] = entry{ip: ip, lastSeen: time.Now()}
	if !existed {
		return true, ""
	}
	return prev.ip != ip, prev.ip
}

// удаляет копии, от которых timeout нет сообщений.Возвращает true, если список изменился
func (t *PeerTable) RemoveExpired(timeout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	changed := false
	deadline := time.Now().Add(-timeout)
	for id, e := range t.peers {
		if e.lastSeen.Before(deadline) {
			delete(t.peers, id)
			changed = true
		}
	}
	return changed
}

//возвращает снимок живых копий, отсортированный по IP 
func (t *PeerTable) Snapshot() []Peer {
	t.mu.Lock()
	defer t.mu.Unlock()

	list := make([]Peer, 0, len(t.peers))
	for id, e := range t.peers {
		list = append(list, Peer{ID: id, IP: e.ip})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IP != list[j].IP {
			return list[i].IP < list[j].IP
		}
		return list[i].ID < list[j].ID
	})
	return list
}