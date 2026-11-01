// Package service собирает данные из API
package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"places/internal/model"
)

// Geocoder ищет локации
type Geocoder interface {
	Search(ctx context.Context, query string) ([]model.Location, error)
}

// WeatherProvider получает погоду
type WeatherProvider interface {
	Current(ctx context.Context, lat, lon float64) (model.Weather, error)
}

// PlacesProvider получает места и их описания
type PlacesProvider interface {
	Nearby(ctx context.Context, lat, lon float64, radiusM, limit int) ([]model.PlaceBrief, error)
	Details(ctx context.Context, xid string) (model.PlaceDetails, error)
}

// Options — параметры поиска
type Options struct {
	RadiusM         int // радиус поиска мест, метры
	PlacesLimit     int // максимум мест в отчёте
	ParallelDetails int // сколько описаний запрашивать одновременно
}

// Service объединяет источники данных
type Service struct {
	geo     Geocoder
	weather WeatherProvider
	places  PlacesProvider
	opts    Options
}

// New создаёт сервис
func New(g Geocoder, w WeatherProvider, p PlacesProvider, opts Options) *Service {
	if opts.ParallelDetails < 1 {
		opts.ParallelDetails = 1
	}
	return &Service{geo: g, weather: w, places: p, opts: opts}
}

// LocationsResult — результат поиска
type LocationsResult struct {
	Locations []model.Location
	Err       error
}

// FindLocations запускает поиск и возвращает канал с результатом
func (s *Service) FindLocations(ctx context.Context, query string) <-chan LocationsResult {
	// буфер позволяет завершить горутину без ожидающего получателя
	out := make(chan LocationsResult, 1)

	go func() {
		query := strings.TrimSpace(query)
		if query == "" {
			out <- LocationsResult{Err: errors.New("пустой запрос")}
			return
		}
		locs, err := s.geo.Search(ctx, query)
		out <- LocationsResult{Locations: locs, Err: err}
	}()

	return out
}

// LoadReport параллельно получает погоду и места, затем собирает отчёт
func (s *Service) LoadReport(ctx context.Context, loc model.Location) <-chan model.Report {
	out := make(chan model.Report, 1)

	go func() {
		report := model.Report{Location: loc}

		var wg sync.WaitGroup
		wg.Add(2)

		// загружаем погоду
		go func() {
			defer wg.Done()
			w, err := s.weather.Current(ctx, loc.Lat, loc.Lon)
			if err != nil {
				report.WeatherErr = err
				return
			}
			report.Weather = &w
		}()

		// загружаем места и их описания
		go func() {
			defer wg.Done()
			report.Places, report.PlacesErr = s.loadPlaces(ctx, loc)
		}()

		// ждём обе ветки перед отправкой отчёта
		wg.Wait()
		out <- report
	}()

	return out
}

// loadPlaces получает места и параллельно загружает описания
func (s *Service) loadPlaces(ctx context.Context, loc model.Location) ([]model.Place, error) {
	briefs, err := s.places.Nearby(ctx, loc.Lat, loc.Lon, s.opts.RadiusM, s.opts.PlacesLimit)
	if err != nil {
		return nil, err
	}

	places := make([]model.Place, len(briefs))

	// ограничиваем число одновременных запросов
	sem := make(chan struct{}, s.opts.ParallelDetails)
	var wg sync.WaitGroup

	for i, b := range briefs {
		places[i].PlaceBrief = b

		wg.Add(1)
		go func(i int, xid string) {
			defer wg.Done()

			sem <- struct{}{}        // занять слот
			defer func() { <-sem }() // освободить слот

			// каждая горутина заполняет свою запись
			d, err := s.places.Details(ctx, xid)
			places[i].PlaceDetails = d
			places[i].DetailsErr = err
		}(i, b.Xid)
	}

	wg.Wait()
	return places, nil
}
