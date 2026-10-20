package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"filetransfer/internal/protocol"
)

// idleTimeout — если от клиента нет данных столько времени то соединение разрывается
const idleTimeout = 30 * time.Second
const maxClients = 64

// Server принимает файлы и сохраняет их в uploadsDir
type Server struct {
	port       string
	uploadsDir string
	nextID     atomic.Int64   //счетчик для нумерации клиентов
	wg         sync.WaitGroup //счётчик горутин чтобы было graceful
	clients    chan struct{}
}

//создаёт сервер, слушающий указанный порт
func New(port, uploadsDir string) *Server {
	return &Server{port: port, uploadsDir: uploadsDir, clients: make(chan struct{}, maxClients)}
}

// Run запускает сервер и работает до отмены ctx
func (s *Server) Run(ctx context.Context) error {
	if err := os.MkdirAll(s.uploadsDir, 0o755); err != nil {
		return fmt.Errorf("создание директории %s: %w", s.uploadsDir, err)
	}

	ln, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("запуск прослушивания: %w", err)
	}
	defer ln.Close()
	log.Printf("сервер слушает %s, файлы сохраняются в %q", ln.Addr(), s.uploadsDir)

	// горутина, которая ждёт отмены контекста и закрывает listener
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept() //принимаем соединение для одного клиента
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			log.Printf("ошибка accept: %v", err)
			time.Sleep(100 * time.Millisecond) // чтобы не крутиться в цикле при постоянной ошибке
			continue
		}

		id := s.nextID.Add(1)
		select {
		case s.clients <- struct{}{}:
		default:
			conn.SetWriteDeadline(time.Now().Add(idleTimeout))
			protocol.WriteStatus(conn, protocol.StatusError)
			conn.Close()
			log.Printf("[клиент #%d %s] отказ: достигнут лимит одновременных клиентов", id, conn.RemoteAddr())
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.clients }()
			s.handle(conn, id)
		}()
	}

	s.wg.Wait()
	return nil
}

// handle обслуживает одного клиента
func (s *Server) handle(conn net.Conn, id int64) {
	defer conn.Close()

	tag := fmt.Sprintf("[клиент #%d %s]", id, conn.RemoteAddr())
	log.Printf("%s подключился", tag)

	status := protocol.StatusOK
	if err := s.receive(conn, tag); err != nil {
		log.Printf("%s ошибка: %v", tag, err)
		status = protocol.StatusError
	}

	// Сообщаем клиенту результат, затем закрываем его
	conn.SetWriteDeadline(time.Now().Add(idleTimeout))
	if err := protocol.WriteStatus(conn, status); err != nil {
		log.Printf("%s ошибка отправки статуса клиенту: %v", tag, err)
		return
	}

	if status == protocol.StatusOK {
		log.Printf("%s передача завершена успешно", tag)
	}
}

// receive читает заголовок и содержимое файла и сохраняет его на диск
func (s *Server) receive(conn net.Conn, tag string) error {
	conn.SetReadDeadline(time.Now().Add(idleTimeout))
	hdr, err := protocol.ReadHeader(conn)
	if err != nil {
		return fmt.Errorf("чтение заголовка: %w", err)
	}

	f, path, err := createUnique(s.uploadsDir, hdr.Name)
	if err != nil {
		return fmt.Errorf("создание файла: %w", err)
	}
	log.Printf("%s принимаю %q (%d байт) -> %s", tag, hdr.Name, hdr.Size, path)

	var received atomic.Int64 //счетчик полученных байт
	meter := startSpeedMeter(tag, &received)

	src := &countingReader{conn: conn, counter: &received}
	n, copyErr := io.Copy(f, io.LimitReader(src, hdr.Size)) //максимум hdr.Size байт

	meter.Stop()
	closeErr := f.Close()

	// Проверяем результат: ошибка копирования/закрытия, затем совпадение размера
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && n != hdr.Size {
		copyErr = fmt.Errorf("получено %d байт из %d", n, hdr.Size)
	}
	if copyErr == nil {
		info, err := os.Stat(path)
		if err != nil {
			copyErr = err
		} else if info.Size() != hdr.Size {
			copyErr = fmt.Errorf("на диске сохранено %d байт из %d", info.Size(), hdr.Size)
		}
	}

	if copyErr != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return errors.Join(copyErr, fmt.Errorf("удаление недокачанного файла: %w", removeErr))
		}
		return copyErr
	}
	return nil
}

// countingReader считает прочитанные байты и обновляет таймаут простоя при каждом чтении
type countingReader struct {
	conn    net.Conn
	counter *atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.conn.SetReadDeadline(time.Now().Add(idleTimeout))
	n, err := r.conn.Read(p)
	r.counter.Add(int64(n))
	return n, err
}
