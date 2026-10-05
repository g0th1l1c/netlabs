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

func main() {
	group := flag.String("group", "", "адрес multicast-группы")
	iface := flag.String("iface", "", "имя сетевого интерфейса ")
	interval := flag.Duration("interval", 2*time.Second, "период рассылки сообщений \"я жив\"")
	timeout := flag.Duration("timeout", 6*time.Second, "через сколько отсутствия считать копию пропавшей")
	flag.Parse()

	if *group == "" {
		log.Fatal("необходимо указать адрес multicast-группы: -group=224.0.0.1:9999")
	}
	if *timeout <= *interval {
		log.Fatal("-timeout должен быть больше -interval")
	}

	cfg := discovery.Config{
		GroupAddr: *group,
		IfaceName: *iface,
		Interval:  *interval,
		Timeout:   *timeout,
	}

	app, err := discovery.New(cfg)
	if err != nil {
		log.Fatalf("ошибка инициализации: %v", err)
	}
	defer app.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	app.Run(ctx)
}
