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

const (
	// readDeadline - как часто прерывать блокирующее чтение чтобы заметить отмену контекста
	readDeadline = time.Second

	// recvBufferSize - размер буфера для входящих пакетов
	recvBufferSize = 4096

	localOnlyTTL = 1
)

type Config struct {
	GroupHost string        // IP multicast группы
	GroupPort int           // порт группы
	IfaceName string        // имя сетевого интерфейса ("" = подобрать автоматически)
	Interval  time.Duration // период рассылки сообщений "я жив"
	Timeout   time.Duration // через сколько отсутствия считать копию пропавшей
}

//само приложение
type App struct {
	cfg     Config
	network string
	iface   *net.Interface //обёртка над интерфейсом

	sendConn *net.UDPConn
	recvConn *net.UDPConn

	selfID string // уникальный ID этой копии
	peers  *PeerTable //таблица известных копий
}

// разбирает конфигурацию и открывает сетевые соединения для приёма и отправки multicast-сообщений
//по сути конструктор
func New(cfg Config) (*App, error) {
	network, groupUDP, err := resolveGroup(cfg.GroupHost, cfg.GroupPort)
	if err != nil {
		return nil, err
	}

	//обрабатываем и получаем интерфейс
	iface, err := resolveInterface(cfg.IfaceName, network)
	if err != nil {
		return nil, err
	}

	// соединение для приёма - слушаем группу на выбранном интерфейсе
	recvConn, err := net.ListenMulticastUDP(network, iface, groupUDP)
	if err != nil {
		return nil, fmt.Errorf("не удалось начать приём multicast: %w", err)
	}
	_ = recvConn.SetReadBuffer(recvBufferSize)

	// явно включаем multicast loopback 
	if err := enableLoopback(recvConn, network); err != nil {
		recvConn.Close()
		return nil, fmt.Errorf("не удалось включить multicast loopback: %w", err)
	}

	// Соединение для отправки
	sendAddr := *groupUDP
	if network == "udp6" && sendAddr.Zone == "" {
		sendAddr.Zone = iface.Name
	}
	sendConn, err := net.DialUDP(network, nil, &sendAddr)
	if err != nil {
		recvConn.Close()
		return nil, fmt.Errorf("не удалось открыть сокет для отправки: %w", err)
	}

	//ограничиваем рассылку локальным сегментом - TTL=1 / Hop Limit=1
	if err := restrictToLocalNetwork(sendConn, network); err != nil {
		recvConn.Close()
		sendConn.Close()
		return nil, fmt.Errorf("не удалось ограничить TTL/hop limit: %w", err)
	}

	selfID, err := randomID()
	if err != nil {
		recvConn.Close()
		sendConn.Close()
		return nil, err
	}

	return &App{
		cfg:      cfg,
		network:  network,
		iface:    iface,
		sendConn: sendConn,
		recvConn: recvConn,
		selfID:   selfID,
		peers:    NewPeerTable(),
	}, nil
}


func (a *App) Close() {
	a.sendConn.Close()
	a.recvConn.Close()
}

// Run запускает рассылку, приём и отслеживание живости копий
func (a *App) Run(ctx context.Context) {
	log.Printf("группа=%s:%d протокол=%s интерфейс=%s свой id=%s",
		a.cfg.GroupHost, a.cfg.GroupPort, a.network, a.iface.Name, a.selfID)

	// Канал сигнал "список изменился"
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

	a.printPeers() // начальное состояние

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

	a.send(payload) //объявляем о себе сразу

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
	if _, err := a.sendConn.Write(payload); err != nil {
		log.Printf("ошибка отправки multicast-сообщения: %v", err)
	}
}

//принимает сообщения от других копий и обновляет таблицу пиров
func (a *App) receiveLoop(ctx context.Context, notifyChanged func()) {
	buf := make([]byte, recvBufferSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Дедлайн чтения
		a.recvConn.SetReadDeadline(time.Now().Add(readDeadline))
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

		// Реальный адрес отправителя берём из самого пакета, а не из содержимого сообщения - пакету доверять нельзя
		
		ip := srcAddr.IP.String()
		changed, oldIP := a.peers.Touch(msg.ID, ip)
		if oldIP != "" && oldIP != ip {
			log.Printf("копия %s: адрес изменился %s -> %s", msg.ID, oldIP, ip)
		}
		if changed {
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

// printPeers печатает список живых копий
func (a *App) printPeers() {
	peers := a.peers.Snapshot()
	if len(peers) == 0 {
		fmt.Println("живых копий не обнаружено")
		return
	}
	fmt.Printf("живые копии (%d):\n", len(peers))
	for _, p := range peers {
		fmt.Printf("  %s (id %s)\n", p.IP, p.ID)
	}
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("не удалось сгенерировать идентификатор: %w", err)
	}
	return hex.EncodeToString(b), nil
}