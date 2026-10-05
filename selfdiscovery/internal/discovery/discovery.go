package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"time"
)

// Config - параметры запуска обнаружения копий приложения
type Config struct {
	GroupAddr string        // адрес multicast-группы
	IfaceName string        // имя сетевого интерфейса ("" = подобрать автоматически)
	Interval  time.Duration // период рассылки сообщений "я жив"
	Timeout   time.Duration // через сколько отсутствия считать копию пропавшей
}

// App - обнаружитель копий приложения
type App struct {
	cfg     Config
	network string
	iface   *net.Interface

	sender   *multicastSender
	recvConn *net.UDPConn

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

	// Соединение для приёма
	recvConn, err := net.ListenMulticastUDP(network, iface, groupUDP)
	if err != nil {
		return nil, fmt.Errorf("не удалось начать приём multicast: %w", err)
	}
	_ = recvConn.SetReadBuffer(64 * 1024)

	// Соединение для отправки
	sender, err := newMulticastSender(network, groupUDP, iface)
	if err != nil {
		recvConn.Close()
		return nil, err
	}

	selfID, err := randomID()
	if err != nil {
		recvConn.Close()
		sender.Close()
		return nil, err
	}

	return &App{
		cfg:      cfg,
		network:  network,
		iface:    iface,
		sender:   sender,
		recvConn: recvConn,
		selfID:   selfID,
		peers:    NewPeerTable(),
	}, nil
}

// Close закрывает сетевые соединения
func (a *App) Close() {
	a.sender.Close()
	a.recvConn.Close()
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

		// Дедлайн чтения нужен чтобы периодически проверять отмену контекста, не блокируясь на чтении навсегда
		a.recvConn.SetReadDeadline(time.Now().Add(time.Second))
		n, srcAddr, err := a.recvConn.ReadFromUDP(buf)
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
			continue // чужой/битый пакет в той же группе - игнорируем
		}
		if msg.ID == a.selfID {
			continue // собственное сообщение, вернувшееся по multicast loopback
		}

		if a.peers.Touch(srcAddr.IP.String()) {
			notifyChanged()
		}
	}
}

//периодически убирает из таблицы копии, от которых давно нет вестей.
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
