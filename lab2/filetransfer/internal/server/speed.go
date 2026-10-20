package server

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

//как часто выводится скорость
const reportInterval = 3 * time.Second

//раз в 3 секунды печатает мгновенную и среднюю скорость приёма для одного клиента
type speedMeter struct {
	tag     string
	counter *atomic.Int64 // сколько байт принято на данный момент
	start   time.Time
	stop    chan struct{} //канал для остановки к измерителю
	done    chan struct{} //измеритель закончил
}

func startSpeedMeter(tag string, counter *atomic.Int64) *speedMeter {
	m := &speedMeter{
		tag:     tag,
		counter: counter,
		start:   time.Now(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go m.run()
	return m
}

// Stop останавливает измеритель и дожидается завершения его горутины
func (m *speedMeter) Stop() {
	close(m.stop)
	<-m.done
}

func (m *speedMeter) run() {
	defer close(m.done)

	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	var lastBytes int64 //сколько байт было на момент прошлого отчёта
	lastTime := m.start //время прошлого отчёта
	printed := false

	report := func(now time.Time) {
		total := m.counter.Load()
		instant := rate(total-lastBytes, now.Sub(lastTime))
		average := rate(total, now.Sub(m.start))
		log.Printf("%s мгновенная: %s, средняя: %s (принято %d байт)",
			m.tag, formatSpeed(instant), formatSpeed(average), total)
		lastBytes, lastTime, printed = total, now, true
	}

	for {
		select {
		case now := <-ticker.C:
			report(now)
		case <-m.stop:
			if !printed {
				report(time.Now())
			}
			return
		}
	}
}

func rate(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) / d.Seconds()
}

func formatSpeed(bytesPerSec float64) string {
	units := []string{"Б/с", "КиБ/с", "МиБ/с", "ГиБ/с", "ТиБ/с"} // 1,2^10,2^20,...
	i := 0
	for bytesPerSec >= 1024 && i < len(units)-1 {
		bytesPerSec /= 1024
		i++
	}
	return fmt.Sprintf("%.2f %s", bytesPerSec, units[i])
}
