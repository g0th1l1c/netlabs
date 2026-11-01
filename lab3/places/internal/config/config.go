// Package config загружает настройки приложения
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config хранит настройки приложения
type Config struct {
	GraphHopperKey string
	OpenWeatherKey string
	OpenTripMapKey string

	RadiusM         int           // радиус поиска мест, метры
	PlacesLimit     int           // максимум мест в отчёте
	ParallelDetails int           // сколько описаний грузить одновременно
	HTTPTimeout     time.Duration // таймаут одного запроса
}

// Load читает настройки из окружения и файла .env
func Load() (Config, error) {
	loadDotEnv(".env")

	cfg := Config{
		GraphHopperKey:  os.Getenv("GRAPHHOPPER_KEY"),
		OpenWeatherKey:  os.Getenv("OPENWEATHER_KEY"),
		OpenTripMapKey:  os.Getenv("OPENTRIPMAP_KEY"),
		RadiusM:         intEnv("PLACES_RADIUS_M", 1000),
		PlacesLimit:     intEnv("PLACES_LIMIT", 10),
		ParallelDetails: intEnv("PLACES_PARALLEL", 5),
		HTTPTimeout:     time.Duration(intEnv("HTTP_TIMEOUT_SEC", 10)) * time.Second,
	}

	var missing []string
	if cfg.GraphHopperKey == "" {
		missing = append(missing, "GRAPHHOPPER_KEY")
	}
	if cfg.OpenWeatherKey == "" {
		missing = append(missing, "OPENWEATHER_KEY")
	}
	if cfg.OpenTripMapKey == "" {
		missing = append(missing, "OPENTRIPMAP_KEY")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("не заданы ключи API: %s (см. .env.example)", strings.Join(missing, ", "))
	}
	if cfg.RadiusM <= 0 || cfg.PlacesLimit <= 0 || cfg.ParallelDetails <= 0 || cfg.HTTPTimeout <= 0 {
		return cfg, fmt.Errorf("числовые настройки должны быть положительными")
	}
	return cfg, nil
}

// intEnv читает целое число или возвращает значение по умолчанию
func intEnv(name string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return def
	}
	return v
}

// loadDotEnv загружает переменные из файла, не заменяя заданные в системе
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
