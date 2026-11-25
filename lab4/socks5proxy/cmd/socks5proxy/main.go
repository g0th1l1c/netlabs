// socks5proxy — неблокирующийся SOCKS5-прокси.
package main

import (
	"fmt"
	"log"
	"net/netip"
	"os"
	"socks5proxy/internal/dns"
	"socks5proxy/internal/proxy"
	"strconv"
	"syscall"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "использование: %s <порт> [DNS_IPv4:порт]\n", os.Args[0])
		os.Exit(2)
	}
	port, err := strconv.Atoi(os.Args[1])
	if err != nil || port < 1 || port > 65535 {
		log.Fatal("некорректный порт")
	}
	var server syscall.SockaddrInet4
	if len(os.Args) == 3 {
		server, err = parseResolver(os.Args[2])
	} else {
		server.Addr, err = dns.SystemServer()
		server.Port = 53
	}
	if err != nil {
		log.Fatal(err)
	}
	srv, err := proxy.New(port, server)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(srv.Run())
}

func parseResolver(value string) (syscall.SockaddrInet4, error) {
	addr, err := netip.ParseAddrPort(value)
	if err != nil || !addr.Addr().Is4() || addr.Port() == 0 {
		return syscall.SockaddrInet4{}, fmt.Errorf("некорректный DNS-адрес %q: ожидается IPv4:порт (1–65535)", value)
	}
	return syscall.SockaddrInet4{Addr: addr.Addr().As4(), Port: int(addr.Port())}, nil
}
