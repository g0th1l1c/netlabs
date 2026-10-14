package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Config - параметры запуска обнаружения копий приложения
type Config struct {
	GroupAddr string        // адрес multicast-группы
	IfaceName string        // имя сетевого интерфейса ("" = подобрать автоматически)
	Interval  time.Duration // период рассылки сообщений "я жив"
	Timeout   time.Duration // через сколько отсутствия считать копию пропавшей
}

type App struct {
	cfg     Config
	network string
	iface   *net.Interface

	sender *multicastSender //сокет отправки и обертки над ним 

	// сокет приема и обертки над ним
	recvConn *net.UDPConn
	recvPC4  *ipv4.PacketConn
	recvPC6  *ipv6.PacketConn

	selfID string
	peers  *PeerTable
}

// New разбирает конфигурацию и открывает сетевые соединения для приёма
func New(cfg Config) (*App, error) {
	network, groupUDP, err := resolveGroup(cfg.GroupAddr)
	if err != nil {
		return nil, err
	}

	iface, err := resolveInterface(cfg.IfaceName, network)
	if err != nil {
		return nil, err
	}

	// Принимающий сокет создаём вручную 
	recvConn, err := listenUDPReuse(network, groupUDP.Port)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать принимающий UDP-сокет: %w", err)
	}
	_ = recvConn.SetReadBuffer(64 * 1024)

	app := &App{
		cfg:      cfg,
		network:  network,
		iface:    iface,
		recvConn: recvConn,
		selfID:   "",
		peers:    NewPeerTable(),
	}

	switch network {
	case "udp4":
		//обертка над сокетом которая умеет работать с multicast
		pc := ipv4.NewPacketConn(recvConn)
		if err := pc.JoinGroup(iface, groupUDP); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось присоединиться к группе %s на %s: %w",
				groupUDP, iface.Name, err)
		}
		if err := pc.SetMulticastLoopback(true); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось включить multicast loopback: %w", err)
		}
		if err := pc.SetMulticastInterface(iface); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось установить принимающий интерфейс: %w", err)
		}
		app.recvPC4 = pc

	case "udp6":
		pc := ipv6.NewPacketConn(recvConn)
		if err := pc.JoinGroup(iface, groupUDP); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось присоединиться к группе %s на %s: %w",
				groupUDP, iface.Name, err)
		}
		if err := pc.SetMulticastLoopback(true); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось включить multicast loopback: %w", err)
		}
		if err := pc.SetMulticastInterface(iface); err != nil {
			recvConn.Close()
			return nil, fmt.Errorf("не удалось установить принимающий интерфейс: %w", err)
		}
		app.recvPC6 = pc

	default:
		recvConn.Close()
		return nil, fmt.Errorf("неизвестный тип сети %q", network)
	}

	// Отправитель
	sender, err := newMulticastSender(network, groupUDP, iface)
	if err != nil {
		recvConn.Close()
		return nil, err
	}
	app.sender = sender

	//уникальный айдишник
	selfID, err := randomID()
	if err != nil {
		recvConn.Close()
		sender.Close()
		return nil, err
	}
	app.selfID = selfID

	return app, nil
}

// listenUDPReuse создаёт UDP-сокет с SO_REUSEADDR
func listenUDPReuse(network string, port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(
					syscall.Handle(fd),
					syscall.SOL_SOCKET,
					syscall.SO_REUSEADDR,
					1,
				)
			})
			if err != nil {
				return err
			}
			return serr
		},
	}

	var addr string
	if network == "udp4" {
		addr = fmt.Sprintf("0.0.0.0:%d", port)
	} else {
		addr = fmt.Sprintf("[::]:%d", port)
	}

	pc, err := lc.ListenPacket(context.Background(), network, addr)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// Close закрывает сетевые соединения
func (a *App) Close() {
	if a.sender != nil {
		a.sender.Close()
	}
	if a.recvConn != nil {
		a.recvConn.Close()
	}
}

// Run запускает рассылку, приём и отслеживание "живости" копий
func (a *App) Run(ctx context.Context) {
	log.Printf("группа=%s протокол=%s интерфейс=%s свой id=%s",
		a.cfg.GroupAddr, a.network, a.iface.Name, a.selfID)

	// Канал-сигнал "список изменился"
	changed := make(chan struct{}, 1)
	notifyChanged := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}

	go a.sendLoop(ctx)
	go a.receiveLoop(ctx, notifyChanged)
	go a.expireLoop(ctx, notifyChanged)

	a.printPeers()

	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			a.printPeers()
		}
	}
}

// sendLoop периодически рассылает в группу сообщение "я жив"
func (a *App) sendLoop(ctx context.Context) {
	msg := newMessage(a.selfID)
	payload, err := msg.encode()
	if err != nil {
		log.Printf("ошибка кодирования сообщения: %v", err)
		return
	}

	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()

	a.send(payload) // не ждём первого тика, объявляем о себе сразу

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.send(payload)
		}
	}
}

func (a *App) send(payload []byte) {
	if err := a.sender.Send(payload); err != nil {
		log.Printf("ошибка отправки multicast-сообщения: %v", err)
	}
}

// receiveLoop принимает сообщения от других копий и обновляет таблицу пиров
func (a *App) receiveLoop(ctx context.Context, notifyChanged func()) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Дедлайн чтения нужен чтобы периодически проверять отмену контекста,
		// не блокируясь на чтении навсегда
		a.recvConn.SetReadDeadline(time.Now().Add(time.Second))

		var n int
		var err error
		switch a.network {
		case "udp4":
			n, _, _, err = a.recvPC4.ReadFrom(buf)
		case "udp6":
			n, _, _, err = a.recvPC6.ReadFrom(buf)
		default:
			return
		}

		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("ошибка приёма multicast-сообщения: %v", err)
				continue
			}
		}

		msg, err := decodeMessage(buf[:n])
		if err != nil || !msg.valid() {
			continue 
		}
		if msg.ID == a.selfID {
			continue // собственное сообщение
		}

		if a.peers.Touch(msg.ID) {
			notifyChanged()
		}
	}
}

// expireLoop периодически убирает из таблицы копии, от которых давно нет вестей
func (a *App) expireLoop(ctx context.Context, notifyChanged func()) {
	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.peers.RemoveExpired(a.cfg.Timeout) {
				notifyChanged()
			}
		}
	}
}

func (a *App) printPeers() {
	list := a.peers.List()
	if len(list) == 0 {
		fmt.Println("живых копий не обнаружено")
		return
	}
	fmt.Printf("живые копии (%d): %v\n", len(list), list)
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("не удалось сгенерировать идентификатор: %w", err)
	}
	return hex.EncodeToString(b), nil
}