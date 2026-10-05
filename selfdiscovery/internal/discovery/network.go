package discovery

import (
	"fmt"
	"net"
)

// разбирает адрес multicast-группы 
func resolveGroup(addr string) (network string, udpAddr *net.UDPAddr, err error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", nil, fmt.Errorf("некорректный адрес группы %q: %w", addr, err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return "", nil, fmt.Errorf("не удалось разобрать IP-адрес из %q", host)
	}
	if !ip.IsMulticast() {
		return "", nil, fmt.Errorf("адрес %s не является multicast-адресом", ip)
	}

	if ip.To4() != nil {
		network = "udp4"
	} else {
		network = "udp6"
	}

	udpAddr, err = net.ResolveUDPAddr(network, addr)
	if err != nil {
		return "", nil, fmt.Errorf("не удалось разрешить адрес %q: %w", addr, err)
	}
	return network, udpAddr, nil
}

// возвращает интерфейс по имени  либо подбирает подходящий автоматически
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

// подбирает первый подходящий сетевой интерфейс
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
			ifi := ifi // копия для безопасного взятия адреса
			return &ifi, nil
		}
	}
	return nil, fmt.Errorf("не найден подходящий сетевой интерфейс для %s (укажите его явно через -iface)", network)
}

// проверяет, есть ли у интерфейса адрес нужного семейства 
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
