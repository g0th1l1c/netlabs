# selfdiscovery

Приложение обнаруживает копии самого себя в локальной сети через multicast
UDP-рассылку и печатает список IP-адресов "живых" копий при каждом
изменении (появлении/исчезновении).

## Сборка

```
go mod tidy    # один раз, нужен интернет: подтянет golang.org/x/net
go build -o selfdiscovery ./cmd/selfdiscovery
```

## Запуск

```
./selfdiscovery -group=239.255.0.1:9999
./selfdiscovery -group=[ff02::1]:9999                 # IPv6, протокол определяется автоматически
./selfdiscovery -group=239.255.0.1:9999 -iface=eth0 -interval=1s -timeout=3s
```

Флаги:

| Флаг        | По умолчанию | Назначение                                             |
|-------------|---------------|---------------------------------------------------------|
| `-group`    | (обязателен)  | адрес multicast-группы, напр. `224.0.0.1:9999`          |
| `-iface`    | автоопределение | имя сетевого интерфейса                               |
| `-interval` | `2s`          | период рассылки "я жив"                                 |
| `-timeout`  | `6s`          | через сколько отсутствия считать копию пропавшей         |

## Зависимости

Стандартный пакет `net` умеет слушать multicast-группу
(`net.ListenMulticastUDP`), но не даёт управлять параметрами
*отправки* multicast-трафика: нельзя явно выбрать исходящий интерфейс,
задать TTL/HopLimit именно для multicast-пакетов или включить/выключить
multicast loopback на уровне сокета. Это умеет только
`golang.org/x/net/ipv4` и `golang.org/x/net/ipv6` — отсюда единственная
внешняя зависимость проекта.

## Структура

```
go.mod
cmd/selfdiscovery/main.go          — флаги запуска, точка входа
internal/discovery/message.go      — формат сообщения "я жив"
internal/discovery/network.go      — разбор адреса группы, выбор IPv4/IPv6 и интерфейса
internal/discovery/multicast.go    — отправляющий сокет: интерфейс, TTL/HopLimit, loopback
internal/discovery/peers.go        — потокобезопасная таблица живых пиров
internal/discovery/discovery.go    — рассылка, приём, отслеживание таймаутов
```

