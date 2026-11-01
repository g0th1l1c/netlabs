// Package service — бизнес-логика приложения.
//
// Вся логика задания оформлена в двух функциях, которые возвращают
// «будущий результат» (канал) и сразу же выходят, ничего не блокируя:
//
//	FindLocations — пункт 2 задания (поиск локаций);
//	LoadReport    — пункты 4 и 5 (погода + места + описания).
//
// Пакет не знает ни про HTTP, ни про консоль: он работает с интерфейсами,
// поэтому его легко тестировать подставными (fake) реализациями.
package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"places/internal/model"
)

// Geocoder ищет локации по названию.
type Geocoder interface {
	Search(ctx context.Context, query string) ([]model.Location, error)
}

// WeatherProvider возвращает текущую погоду.
type WeatherProvider interface {
	Current(ctx context.Context, lat, lon float64) (model.Weather, error)
}

// PlacesProvider ищет интересные места и их описания.
type PlacesProvider interface {
	Nearby(ctx context.Context, lat, lon float64, radiusM, limit int) ([]model.PlaceBrief, error)
	Details(ctx context.Context, xid string) (model.PlaceDetails, error)
}

// Options — настройки поиска.
type Options struct {
	RadiusM         int // радиус поиска мест, метры
	PlacesLimit     int // максимум мест в отчёте
	ParallelDetails int // сколько описаний запрашивать одновременно
}

// Service связывает три источника данных.
type Service struct {
	geo     Geocoder
	weather WeatherProvider
	places  PlacesProvider
	opts    Options
}

// New создаёт сервис.
func New(g Geocoder, w WeatherProvider, p PlacesProvider, opts Options) *Service {
	if opts.ParallelDetails < 1 {
		opts.ParallelDetails = 1
	}
	return &Service{geo: g, weather: w, places: p, opts: opts}
}

// LocationsResult — результат поиска локаций.
type LocationsResult struct {
	Locations []model.Location
	Err       error
}

// FindLocations запускает поиск локаций и сразу возвращает канал,
// из которого можно позже получить ровно один результат.
func (s *Service) FindLocations(ctx context.Context, query string) <-chan LocationsResult {
	// Буфер 1: горутина положит результат и завершится, даже если
	// получатель уже ушёл (например, отменил запрос). Утечки горутин нет.
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

// LoadReport собирает полный отчёт по локации и возвращает канал
// с единственным результатом.
//
// Погода и интересные места не зависят друг от друга — они грузятся
// одновременно. Описания мест зависят от списка мест, поэтому запрашиваются
// только после него, но тоже одновременно друг с другом.
// Отчёт отдаётся целиком, только когда готовы все части.
func (s *Service) LoadReport(ctx context.Context, loc model.Location) <-chan model.Report {
	out := make(chan model.Report, 1)

	go func() {
		report := model.Report{Location: loc}

		var wg sync.WaitGroup
		wg.Add(2)

		// Ветка 1: погода.
		go func() {
			defer wg.Done()
			w, err := s.weather.Current(ctx, loc.Lat, loc.Lon)
			if err != nil {
				report.WeatherErr = err
				return
			}
			report.Weather = &w
		}()

		// Ветка 2: места, а затем описания (цепочка зависимых вызовов).
		go func() {
			defer wg.Done()
			report.Places, report.PlacesErr = s.loadPlaces(ctx, loc)
		}()

		// Каждая ветка пишет в своё поле отчёта, поэтому гонки данных нет.
		// Ждём обе ветки здесь, внутри горутины — вызывающий код не блокируется.
		wg.Wait()
		out <- report
	}()

	return out
}

// loadPlaces: список мест -> описание каждого места (параллельно).
func (s *Service) loadPlaces(ctx context.Context, loc model.Location) ([]model.Place, error) {
	briefs, err := s.places.Nearby(ctx, loc.Lat, loc.Lon, s.opts.RadiusM, s.opts.PlacesLimit)
	if err != nil {
		return nil, err
	}

	places := make([]model.Place, len(briefs))

	// sem ограничивает число одновременных запросов: бесплатные тарифы API
	// не любят, когда на них обрушивается десяток запросов разом.
	sem := make(chan struct{}, s.opts.ParallelDetails)
	var wg sync.WaitGroup

	for i, b := range briefs {
		places[i].PlaceBrief = b

		wg.Add(1)
		go func(i int, xid string) {
			defer wg.Done()

			sem <- struct{}{}        // занять «слот»
			defer func() { <-sem }() // освободить «слот»

			// Горутина i пишет только в places[i]: порядок мест сохраняется,
			// а мьютекс не нужен.
			d, err := s.places.Details(ctx, xid)
			places[i].PlaceDetails = d
			places[i].DetailsErr = err
		}(i, b.Xid)
	}

	wg.Wait()
	return places, nil
}
