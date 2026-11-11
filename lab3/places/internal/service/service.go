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
	SearchAsync(ctx context.Context, query string, done func([]model.Location, error))
}

// WeatherProvider получает погоду
type WeatherProvider interface {
	CurrentAsync(ctx context.Context, lat, lon float64, done func(model.Weather, error))
}

// PlacesProvider получает места и их описания
type PlacesProvider interface {
	NearbyAsync(ctx context.Context, lat, lon float64, radiusM, limit int, done func([]model.PlaceBrief, error))
	DetailsAsync(ctx context.Context, xid string, done func(model.PlaceDetails, error))
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

// возвращает канал с конечным результатом поиска
func (s *Service) FindLocations(ctx context.Context, query string) <-chan LocationsResult {
	out := make(chan LocationsResult, 1)
	query = strings.TrimSpace(query)
	if query == "" {
		out <- LocationsResult{Err: errors.New("пустой запрос")}
		close(out) //закрывам канал - никто не может туда писать
		return out
	}
	s.geo.SearchAsync(ctx, query, func(locs []model.Location, err error) {
		out <- LocationsResult{Locations: locs, Err: err}
		close(out)
	})
	return out
}

// отправляет отчёт 
func (s *Service) LoadReport(ctx context.Context, loc model.Location) <-chan model.Report {
	out := make(chan model.Report, 1)
	report := model.Report{Location: loc}
	var mu sync.Mutex
	remaining := 2 //для 2 независимых задач
	finish := func(update func()) {
		mu.Lock()
		update()
		remaining--
		if remaining == 0 {
			out <- report
			close(out)
		}
		mu.Unlock()
	}
	
	s.weather.CurrentAsync(ctx, loc.Lat, loc.Lon, func(w model.Weather, err error) { //погода
		finish(func() {
			report.WeatherErr = err
			if err == nil {
				report.Weather = &w
			}
		})
	})
	s.loadPlaces(ctx, loc, func(p []model.Place, err error) { //достопримечательности
		finish(func() { report.Places = p; report.PlacesErr = err })
	})
	return out
}

// получение списка запускает цепочки описаний без ожидания в вызывающем коде
func (s *Service) loadPlaces(ctx context.Context, loc model.Location, done func([]model.Place, error)) {
	s.places.NearbyAsync(ctx, loc.Lat, loc.Lon, s.opts.RadiusM, s.opts.PlacesLimit, func(briefs []model.PlaceBrief, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		places := make([]model.Place, len(briefs))
		if len(briefs) == 0 {
			done(places, nil)
			return
		}
		var mu sync.Mutex
		next, remaining := 0, len(briefs)
		//next - индекс следующего места, подробности которого ещё не начали загружать
		//remaining - сколько запросов подробностей ещё не завершилось, включая те, которые пока не запущены
		var launch func() //предварительно объявляем переменную чтобы впредь могли ссылаться на саму себя
		launch = func() { //загружаем подробности о достопримечательности
			mu.Lock()
			if next == len(briefs) {
				mu.Unlock()
				return
			}
			i := next
			next++
			mu.Unlock()
			complete := func(d model.PlaceDetails, err error) {//когда завершится загрузка подробностей одного места
				mu.Lock()
				places[i] = model.Place{PlaceBrief: briefs[i], PlaceDetails: d, DetailsErr: err}
				remaining--
				last := remaining == 0
				mu.Unlock()
				if last { 
					done(places, nil)//вызывается после обработки всех достопримечательностей
					return
				}
				// продолжение запускает следующий запрос после освобождения места
				go launch()
			}
			if err := ctx.Err(); err != nil {
				complete(model.PlaceDetails{}, err)
				return
			}
			s.places.DetailsAsync(ctx, briefs[i].Xid, complete)
		}
		for i := 0; i < s.opts.ParallelDetails && i < len(briefs); i++ {
			launch()
		}
	})
}
