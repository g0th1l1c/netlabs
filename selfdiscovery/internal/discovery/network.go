package discovery

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

//проверяет адрес multicast-группы и определяет тип сети
func resolveGroup(host string, port int) (network string, udpAddr *net.UDPAddr, err error) {
	if port < 1 || port > 65535 {
		return "", nil, fmt.Errorf("некорректный порт %d: допустимый диапазон 1-65535", port)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		if strings.Contains(host, ":") {
			return "", nil, fmt.Errorf("в -group указывайте только IP-адрес (без порта), порт передаётся отдельно через -port")
		}
		return "", nil, fmt.Errorf("не удалось разобрать IP-адрес %q", host)
	}

	if !ip.IsMulticast() {
		return "", nil, fmt.Errorf("адрес %s не является multicast-адресом", ip)
	}

	if ip.To4() != nil {
		network = "udp4"
	} else {
		network = "udp6"
	}

	udpAddr, err = net.ResolveUDPAddr(network, net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return "", nil, fmt.Errorf("не удалось разрешить адрес группы: %w", err)
	}
	return network, udpAddr, nil
}
//включает multicast loopback на принимающем сокете
func enableLoopback(conn *net.UDPConn, network string) error {
	if network == "udp4" {
		return ipv4.NewPacketConn(conn).SetMulticastLoopback(true)
	}
	return ipv6.NewPacketConn(conn).SetMulticastLoopback(true)
}

//  ограничивает multicast-рассылку локальным сегментом
func restrictToLocalNetwork(conn *net.UDPConn, network string) error {
	if network == "udp4" {
		return ipv4.NewPacketConn(conn).SetTTL(localOnlyTTL)
	}
	return ipv6.NewPacketConn(conn).SetHopLimit(localOnlyTTL)
}

// возвращает интерфейс по имени (если он задан явно) либо подбирает подходящий автоматически
func resolveInterface(name, network string) (*net.Interface, error) {
	if name != "" {
		ifi, err := net.InterfaceByName(name)
		if err != nil {
			return nil, fmt.Errorf("интерфейс %q не найден: %w", name, err)
		}
		if ifi.Flags&net.FlagUp == 0 {
			return nil, fmt.Errorf("интерфейс %q выключен", name)
		}
		if ifi.Flags&net.FlagMulticast == 0 {
			return nil, fmt.Errorf("интерфейс %q не поддерживает multicast", name)
		}
		return ifi, nil
	}
	return pickInterface(network)
}

//подбирает первый подходящий сетевой интерфейс: поднятый,поддерживающий multicast, не loopback и имеющий адрес нужного семейства
func pickInterface(network string) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список интерфейсов: %w", err)
	}

	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		if ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if interfaceHasFamily(ifi, network) {
			return &ifi, nil
		}
	}
	return nil, fmt.Errorf("не найден подходящий сетевой интерфейс для %s (укажите его явно через -iface)", network)
}

//проверяет, есть ли у интерфейса адрес нужного семейства (IPv4/IPv6)
func interfaceHasFamily(ifi net.Interface, network string) bool {
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		isV4 := ipNet.IP.To4() != nil
		if network == "udp4" && isV4 {
			return true
		}
		if network == "udp6" && !isV4 {
			return true
		}
	}
	return false
}