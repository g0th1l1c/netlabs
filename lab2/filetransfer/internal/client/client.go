package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"filetransfer/internal/protocol"
)

const (
	dialTimeout     = 10 * time.Second // времня на соединение с сервером(рукопожатие)
	responseTimeout = 30 * time.Second //сколько ждать ответа сервера после отправки файла
	writeTimeout    = 30 * time.Second
)

//отправляет файл filePath на сервер host:port и ждёт ответа о результате
//возвращает nil  если сервер подтвердил успешное сохранение
func Send(filePath, host, port string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("открытие файла: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("получение информации о файле: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q не является обычным файлом", filePath)
	}
	if info.Size() < 0 || info.Size() > protocol.MaxFileSize {
		return fmt.Errorf("размер файла должен быть не больше %d байт", protocol.MaxFileSize)
	}

	hdr := protocol.Header{Name: filepath.Base(filePath), Size: info.Size()}
	//само подключение к серверу - рукопожатие
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), dialTimeout)
	if err != nil {
		return fmt.Errorf("подключение к серверу: %w", err)
	}
	defer conn.Close()

	writer := deadlineWriter{Conn: conn, timeout: writeTimeout}
	if err := protocol.WriteHeader(writer, hdr); err != nil {
		return fmt.Errorf("отправка заголовка: %w", err)
	}

	//вернёт ошибку, если файл оказался короче заявленного размера
	if _, err := io.CopyN(writer, f, hdr.Size); err != nil {
		return fmt.Errorf("отправка содержимого: %w", err)
	}
	finalInfo, err := f.Stat()
	if err != nil {
		return fmt.Errorf("повторная проверка файла: %w", err)
	}
	if finalInfo.Size() != info.Size() || !finalInfo.ModTime().Equal(info.ModTime()) {
		return errors.New("исходный файл изменился во время передачи")
	}

	conn.SetReadDeadline(time.Now().Add(responseTimeout)) //устанавливает таймаут на чтение
	var status [1]byte
	if _, err := io.ReadFull(conn, status[:]); err != nil {
		return fmt.Errorf("получение ответа сервера: %w", err)
	}
	if status[0] != protocol.StatusOK {
		if status[0] != protocol.StatusError {
			return fmt.Errorf("сервер вернул неизвестный статус %d", status[0])
		}
		return errors.New("сервер сообщил об ошибке при сохранении файла")
	}
	return nil
}

type deadlineWriter struct {
	net.Conn
	timeout time.Duration
}

func (w deadlineWriter) Write(p []byte) (int, error) {
	if err := w.Conn.SetWriteDeadline(time.Now().Add(w.timeout)); err != nil {
		return 0, err
	}
	return w.Conn.Write(p)
}
