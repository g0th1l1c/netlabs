package proxy

import "syscall"

// константы протокола socks5 (rfc 1928)
const (
	ver5 = 5

	methodNoAuth     = 0x00
	methodNoAccepted = 0xFF

	cmdConnect = 1

	atypIPv4   = 1
	atypDomain = 3
	atypIPv6   = 4

	repOK               = 0
	repFailure          = 1
	repNetUnreachable   = 3
	repHostUnreachable  = 4
	repRefused          = 5
	repCmdNotSupported  = 7
	repAtypNotSupported = 8
)

// reply собирает ответ сервера с ipv4-адресом привязки
func reply(code byte, addr [4]byte, port uint16) []byte {
	return []byte{ver5, code, 0, atypIPv4, addr[0], addr[1], addr[2], addr[3], byte(port >> 8), byte(port)}
}

// repFromErrno переводит ошибку connect в код ответа socks5
func repFromErrno(err error) byte {
	switch err {
	case syscall.ECONNREFUSED:
		return repRefused
	case syscall.ENETUNREACH:
		return repNetUnreachable
	case syscall.EHOSTUNREACH, syscall.ETIMEDOUT:
		return repHostUnreachable
	}
	return repFailure
}
