package discovery

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const multicastTTL = 1

type multicastSender struct {
	conn    *net.UDPConn
	groupV4 *ipv4.PacketConn
	groupV6 *ipv6.PacketConn
}

func newMulticastSender(network string, group *net.UDPAddr, iface *net.Interface) (*multicastSender, error) {
	conn, err := net.DialUDP(network, nil, group)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть сокет для отправки: %w", err)
	}

	s := &multicastSender{conn: conn}

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

// Send отправляет payload в multicast-группу
func (s *multicastSender) Send(payload []byte) error {
	_, err := s.conn.Write(payload)
	return err
}

func (s *multicastSender) Close() error {
	return s.conn.Close()
}