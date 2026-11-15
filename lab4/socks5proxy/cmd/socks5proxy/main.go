// socks5proxy — неблокирующийся SOCKS5-прокси.
package main

import (
	"fmt"
	"log"
	"os"
	"socks5proxy/internal/dns"
	"socks5proxy/internal/proxy"
	"strconv"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "использование: %s <порт>\n", os.Args[0])
		os.Exit(2)
	}
	port, err := strconv.Atoi(os.Args[1])
	if err != nil || port < 1 || port > 65535 {
		log.Fatal("некорректный порт")
	}
	server, err := dns.SystemServer()  //узнаем адрес dns сервера
	if err != nil {
		log.Fatal(err)
	}
	srv, err := proxy.New(port, server)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(srv.Run())
}
