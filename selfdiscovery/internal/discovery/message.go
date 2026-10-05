package discovery

import "encoding/json"

// метка протокола
const magic = "selfdiscovery-v1"

// message - сообщение, которым копии приложения обмениваются между собой
type message struct {
	Magic string `json:"magic"`
	ID    string `json:"id"`
}

func newMessage(selfID string) message {
	return message{Magic: magic, ID: selfID}
}

func (m message) encode() ([]byte, error) {
	return json.Marshal(m)
}

func decodeMessage(data []byte) (message, error) {
	var m message
	err := json.Unmarshal(data, &m)
	return m, err
}

// valid проверяет, что сообщение принадлежит нашему протоколу и заполнено корректно
func (m message) valid() bool {
	return m.Magic == magic && m.ID != ""
}
