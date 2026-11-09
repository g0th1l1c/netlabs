// Команда places — консольное приложение для поиска интересных мест.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"places/internal/api"
	"places/internal/config"
	"places/internal/httpclient"
	"places/internal/service"
	"places/internal/ui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка настройки:", err)
		os.Exit(1)
	}

	// Ctrl+C отменяет ctx, а вместе с ним и все запросы в полёте.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := httpclient.New(cfg.HTTPTimeout)

	svc := service.New(
		api.NewGraphHopper(client, cfg.GraphHopperKey),
		api.NewOpenWeather(client, cfg.OpenWeatherKey),
		api.NewOpenTripMap(client, cfg.OpenTripMapKey),
		service.Options{
			RadiusM:         cfg.RadiusM,
			PlacesLimit:     cfg.PlacesLimit,
			ParallelDetails: cfg.ParallelDetails,
		},
	)

	ui.NewConsole(svc, os.Stdin, os.Stdout).Run(ctx)
}
