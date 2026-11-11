package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os/exec"
	"places/internal/model"
	"places/internal/service"
	"runtime"
	"time"
)

//go:embed index.html
var page []byte

type Backend interface {
	FindLocations(context.Context, string) <-chan service.LocationsResult
	LoadReport(context.Context, model.Location) <-chan model.Report
}

func Handler(s Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "метод не поддерживается", 405)
			return
		}
		if len(r.URL.Query().Get("q")) > 500 {
			http.Error(w, "слишком длинный запрос", 400)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case result := <-s.FindLocations(r.Context(), r.URL.Query().Get("q")):
			if result.Err != nil {
				http.Error(w, result.Err.Error(), 502)
				return
			}
			writeJSON(w, result.Locations)
		}
	})
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "метод не поддерживается", 405)
			return
		}
		// запрещаем запросы от посторонних сайтов
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "недопустимый источник", 403)
			return
		}
		var loc model.Location
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&loc); err != nil || math.IsNaN(loc.Lat) || math.IsNaN(loc.Lon) || math.Abs(loc.Lat) > 90 || math.Abs(loc.Lon) > 180 {
			http.Error(w, "неверная локация", 400)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case report := <-s.LoadReport(r.Context(), loc):
			type place struct {
				model.PlaceBrief
				model.PlaceDetails
				Error string
			}
			places := make([]place, 0, len(report.Places))
			for _, p := range report.Places {
				places = append(places, place{p.PlaceBrief, p.PlaceDetails, errorText(p.DetailsErr)})
			}
			writeJSON(w, struct {
				Location     model.Location
				Weather      *model.Weather
				WeatherError string
				Places       []place
				PlacesError  string
			}{loc, report.Weather, errorText(report.WeatherErr), places, errorText(report.PlacesErr)})
		}
	})
	return mux
}
func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(value)
}

// запускает локальный интерфейс и открывает браузер
func Run(ctx context.Context, s Backend) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: Handler(s), ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	url := "http://" + listener.Addr().String()
	fmt.Println("Откройте приложение:", url)
	go func() { <-ctx.Done(); server.Close() }()
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err == nil {
		go command.Wait()
	}
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
