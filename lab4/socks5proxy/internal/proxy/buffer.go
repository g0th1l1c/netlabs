package proxy

import (
	"syscall"
)

// размер буфера одного направления (32 КиБ)
const bufSize = 32 * 1024

// buffer — ограниченный буфер: данные лежат в b[r:w]
type buffer struct {
	b    []byte
	r, w int
	//r — позиция, с которой начинаются ещё не отправленные данные
	//w — позиция, на которой заканчиваются записанные данные
}

func newBuffer() buffer { return buffer{b: make([]byte, bufSize)} }

func (b *buffer) readable() []byte { return b.b[b.r:b.w] } //данные которые нужно отправить

func (b *buffer) empty() bool { return b.r == b.w }

// hasSpace сообщает, есть ли место для новых данных
func (b *buffer) hasSpace() bool { return b.r > 0 || b.w < len(b.b) }

// free возвращает свободное место, при необходимости сдвигая данные в начало
func (b *buffer) free() []byte {
	if b.r == b.w {
		b.r, b.w = 0, 0
	} else if b.w == len(b.b) && b.r > 0 {
		b.w = copy(b.b, b.b[b.r:b.w]) //переносим неотправленные байты в начало
		b.r = 0
	}
	return b.b[b.w:]
}

// put добавляет небольшой блок
func (b *buffer) put(p []byte) { b.w += copy(b.free(), p) }

// recv читает из сокета в буфер один раз
// ok= false - фатальная ошибка
func recv(fd int, b *buffer) (eof, ok bool) {
	dst := b.free()
	if len(dst) == 0 {
		return false, true
	}
	n, err := syscall.Read(fd, dst)
	switch {
	case err == syscall.EAGAIN || err == syscall.EINTR: //сейчас нет данных либо прерван сигналом
		return false, true
	case err != nil:
		return false, false
	case n == 0:
		return true, true
	}
	b.w += n
	return false, true
}

// send пишет в сокет один раз содержимое буфера
func send(fd int, b *buffer) (ok bool) {
	src := b.readable()
	if len(src) == 0 {
		return true
	}
	n, err := syscall.Write(fd, src)
	switch {
	case err == syscall.EAGAIN || err == syscall.EINTR:
		return true
	case err != nil:
		return false
	}
	b.r += n
	return true
}
