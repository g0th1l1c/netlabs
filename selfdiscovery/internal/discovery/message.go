package discovery

import (
	"encoding/hex"
	"encoding/json"
)

//метка протокола чтобы отличать "свои" сообщения от случайного постороннего multicast-трафика, попавшего в ту же группу
const magic = "selfdiscovery-v1"

//длина идентификатора копии в hex-виде
const idHexLength = 16

// сообщение, которым копии приложения обмениваются между собой
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

//проверяет, что сообщение принадлежит нашему протоколу и заполнено корректно
func (m message) valid() bool {
	if m.Magic != magic || len(m.ID) != idHexLength {
		return false
	}
	_, err := hex.DecodeString(m.ID)
	return err == nil
}