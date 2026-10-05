package discovery

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const multicastTTL = 1

// multicastSender - сокет, который реально отправляет пакеты в multicast-группу
type multicastSender struct {
	conn    *net.UDPConn // нижележащий сокет, нужен только для Close
	groupV4 *ipv4.PacketConn
	groupV6 *ipv6.PacketConn
	dst     net.Addr
}

// newMulticastSender открывает UDP-сокет на порту и настраивает его как multicast-отправитель
func newMulticastSender(network string, group *net.UDPAddr, iface *net.Interface) (*multicastSender, error) {
	conn, err := net.ListenUDP(network, &net.UDPAddr{}) // IP не задан, порт 0 - выберет система
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть сокет для отправки: %w", err)
	}

	s := &multicastSender{conn: conn, dst: group}

	switch network {
	case "udp4":
		p := ipv4.NewPacketConn(conn)
		if err := p.SetMulticastInterface(iface); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось привязать отправку к интерфейсу %s: %w", iface.Name, err)
		}
		if err := p.SetMulticastTTL(multicastTTL); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось установить multicast TTL: %w", err)
		}
		if err := p.SetMulticastLoopback(true); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось включить multicast loopback: %w", err)
		}
		s.groupV4 = p

	case "udp6":
		p := ipv6.NewPacketConn(conn)
		if err := p.SetMulticastInterface(iface); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось привязать отправку к интерфейсу %s: %w", iface.Name, err)
		}
		if err := p.SetMulticastHopLimit(multicastTTL); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось установить multicast hop limit: %w", err)
		}
		if err := p.SetMulticastLoopback(true); err != nil {
			conn.Close()
			return nil, fmt.Errorf("не удалось включить multicast loopback: %w", err)
		}
		s.groupV6 = p

	default:
		conn.Close()
		return nil, fmt.Errorf("неизвестный тип сети %q", network)
	}

	return s, nil
}

// отправляет payload в multicast-группу через настроенный интерфейс
func (s *multicastSender) Send(payload []byte) error {
	var err error
	switch {
	case s.groupV4 != nil:
		_, err = s.groupV4.WriteTo(payload, nil, s.dst)
	case s.groupV6 != nil:
		_, err = s.groupV6.WriteTo(payload, nil, s.dst)
	}
	return err
}

func (s *multicastSender) Close() error {
	return s.conn.Close()
}
