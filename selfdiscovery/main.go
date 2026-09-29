package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"selfdiscovery/internal/discovery"
)

// порт по умолчанию для обмена сообщениями между копиями
const defaultPort = 9999

func main() {
	group := flag.String("group", "", "IP-адрес multicast-группы")
	port := flag.Int("port", defaultPort, "порт multicast-группы")
	iface := flag.String("iface", "", "имя сетевого интерфейса")
	interval := flag.Duration("interval", 2*time.Second, "период рассылки сообщений \"я жив\"")
	timeout := flag.Duration("timeout", 6*time.Second, "через сколько отсутствия сообщений считать копию пропавшей")
	flag.Parse()

	if *group == "" {
		log.Fatal("необходимо указать адрес multicast-группы: -group=224.0.0.1")
	}
	if *interval <= 0 {
		log.Fatal("-interval должен быть положительным")
	}
	if *timeout <= 0 {
		log.Fatal("-timeout должен быть положительным")
	}
	if *timeout <= *interval {
		log.Fatal("-timeout должен быть больше -interval")
	}

	cfg := discovery.Config{
		GroupHost: *group,
		GroupPort: *port,
		IfaceName: *iface,
		Interval:  *interval,
		Timeout:   *timeout,
	}

	app, err := discovery.New(cfg)
	if err != nil {
		log.Fatalf("ошибка инициализации: %v", err)
	}
	defer app.Close()

	// завершение по Ctrl+C 
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	app.Run(ctx)
}