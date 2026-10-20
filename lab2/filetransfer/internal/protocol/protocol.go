// Клиент -> сервер:
//	| длина имени    | имя файла       | размер файла     | содержимое
//	| 2 байта        | N байт, UTF-8   | 8 байт           | size байт

// Сервер -> клиент (после приёма всех данных):
//
//	| статус         |
//	| 1 байт         |
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	// MaxNameLen — максимальная длина имени файла в байтах
	MaxNameLen = 4096
	// MaxFileSize — максимальный размер файла 1 тебибайт (2^40)
	MaxFileSize int64 = 1 << 40
	// StatusError — сервер сообщает о неуспехе
	StatusError byte = 0
	// StatusOK — сервер сообщает об успешном сохранении файла
	StatusOK byte = 1
)

//заголовок, который клиент отправляет перед содержимым файла
type Header struct {
	Name string // имя файла
	Size int64  // размер содержимого в байтах
}

func (h Header) validate() error {
	if len(h.Name) == 0 || len(h.Name) > MaxNameLen {
		return fmt.Errorf("длина имени файла должна быть от 1 до %d байт, получено %d", MaxNameLen, len(h.Name))
	}
	if !utf8.ValidString(h.Name) {
		return fmt.Errorf("имя файла не является корректной UTF-8 строкой")
	}
	if h.Size < 0 || h.Size > MaxFileSize {
		return fmt.Errorf("размер файла должен быть от 0 до %d байт, получено %d", MaxFileSize, h.Size)
	}
	return nil
}

// WriteHeader кодирует и отправляет заголовок
func WriteHeader(w io.Writer, h Header) error {
	if err := h.validate(); err != nil {
		return err
	}
	buf := make([]byte, 2+len(h.Name)+8) //выделяем буфер
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(h.Name)))
	copy(buf[2:], h.Name)
	binary.BigEndian.PutUint64(buf[2+len(h.Name):], uint64(h.Size))
	return writeFull(w, buf)
}

func WriteStatus(w io.Writer, status byte) error {
	return writeFull(w, []byte{status})
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// ReadHeader читает и проверяет заголовок
func ReadHeader(r io.Reader) (Header, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil { //читаем размер имени
		return Header{}, fmt.Errorf("чтение длины имени: %w", err)
	}
	nameLen := int(binary.BigEndian.Uint16(lenBuf[:])) //из байтов в число
	if nameLen == 0 || nameLen > MaxNameLen {
		return Header{}, fmt.Errorf("недопустимая длина имени файла: %d", nameLen)
	}

	buf := make([]byte, nameLen+8) //буфер для имени и размера
	if _, err := io.ReadFull(r, buf); err != nil {
		return Header{}, fmt.Errorf("чтение имени и размера: %w", err)
	}

	size := binary.BigEndian.Uint64(buf[nameLen:])
	h := Header{Name: string(buf[:nameLen])}
	if size > uint64(MaxFileSize) {
		return Header{}, fmt.Errorf("размер файла %d превышает лимит %d", size, MaxFileSize)
	}
	h.Size = int64(size)

	if err := h.validate(); err != nil {
		return Header{}, err
	}
	return h, nil
}
