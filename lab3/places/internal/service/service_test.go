package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"places/internal/model"
)

// ---- подставные реализации ----

type fakeGeo struct {
	locs []model.Location
	err  error
}

func (f fakeGeo) Search(ctx context.Context, q string) ([]model.Location, error) {
	return f.locs, f.err
}

type fakeWeather struct {
	delay time.Duration
	err   error
}

func (f fakeWeather) Current(ctx context.Context, lat, lon float64) (model.Weather, error) {
	time.Sleep(f.delay)
	if f.err != nil {
		return model.Weather{}, f.err
	}
	return model.Weather{Description: "ясно", TempC: 20}, nil
}

type fakePlaces struct {
	nearbyDelay  time.Duration
	detailsDelay time.Duration
	count        int
	failXid      string

	mu      sync.Mutex
	current int
	maxSeen int
}

func (f *fakePlaces) Nearby(ctx context.Context, lat, lon float64, r, limit int) ([]model.PlaceBrief, error) {
	time.Sleep(f.nearbyDelay)
	var out []model.PlaceBrief
	for i := 0; i < f.count; i++ {
		out = append(out, model.PlaceBrief{Xid: string(rune('a' + i)), Name: "Место " + string(rune('A'+i))})
	}
	return out, nil
}

func (f *fakePlaces) Details(ctx context.Context, xid string) (model.PlaceDetails, error) {
	f.mu.Lock()
	f.current++
	if f.current > f.maxSeen {
		f.maxSeen = f.current
	}
	f.mu.Unlock()

	time.Sleep(f.detailsDelay)

	f.mu.Lock()
	f.current--
	f.mu.Unlock()

	if xid == f.failXid {
		return model.PlaceDetails{}, errors.New("boom")
	}
	return model.PlaceDetails{Description: "описание " + xid}, nil
}

// ---- тесты ----

func TestFindLocations(t *testing.T) {
	want := []model.Location{{Name: "X"}}
	s := New(fakeGeo{locs: want}, fakeWeather{}, &fakePlaces{}, Options{})

	res := <-s.FindLocations(context.Background(), "  что-то ")
	if res.Err != nil || len(res.Locations) != 1 {
		t.Fatalf("неожиданный результат: %+v", res)
	}
}

func TestFindLocationsEmptyQuery(t *testing.T) {
	s := New(fakeGeo{}, fakeWeather{}, &fakePlaces{}, Options{})
	res := <-s.FindLocations(context.Background(), "   ")
	if res.Err == nil {
		t.Fatal("ожидалась ошибка для пустого запроса")
	}
}

func TestFindLocationsError(t *testing.T) {
	s := New(fakeGeo{err: errors.New("fail")}, fakeWeather{}, &fakePlaces{}, Options{})
	res := <-s.FindLocations(context.Background(), "x")
	if res.Err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

// Погода (200 мс) и цепочка «места (100 мс) -> описания (100 мс)» идут
// одновременно, поэтому всё занимает ~200 мс, а не 200+100+100 = 400 мс.
func TestLoadReportRunsInParallel(t *testing.T) {
	fp := &fakePlaces{nearbyDelay: 100 * time.Millisecond, detailsDelay: 100 * time.Millisecond, count: 5}
	s := New(fakeGeo{}, fakeWeather{delay: 200 * time.Millisecond}, fp,
		Options{RadiusM: 1000, PlacesLimit: 5, ParallelDetails: 5})

	start := time.Now()
	report := <-s.LoadReport(context.Background(), model.Location{})
	elapsed := time.Since(start)

	if elapsed > 330*time.Millisecond {
		t.Fatalf("слишком долго (%v): вызовы выполняются не параллельно", elapsed)
	}
	if report.Weather == nil || len(report.Places) != 5 {
		t.Fatalf("неполный отчёт: %+v", report)
	}
}

func TestLoadReportKeepsOrderAndDescriptions(t *testing.T) {
	fp := &fakePlaces{count: 4}
	s := New(fakeGeo{}, fakeWeather{}, fp, Options{ParallelDetails: 4})

	report := <-s.LoadReport(context.Background(), model.Location{})
	for i, p := range report.Places {
		wantXid := string(rune('a' + i))
		if p.Xid != wantXid || p.Description != "описание "+wantXid {
			t.Fatalf("место %d: %+v", i, p)
		}
	}
}

func TestLoadReportPartialFailures(t *testing.T) {
	fp := &fakePlaces{count: 3, failXid: "b"}
	s := New(fakeGeo{}, fakeWeather{err: errors.New("no weather")}, fp, Options{ParallelDetails: 3})

	report := <-s.LoadReport(context.Background(), model.Location{})

	if report.Weather != nil || report.WeatherErr == nil {
		t.Fatal("ошибка погоды должна быть сохранена в отчёте")
	}
	if len(report.Places) != 3 {
		t.Fatalf("места должны сохраниться, есть %d", len(report.Places))
	}
	if report.Places[1].DetailsErr == nil || report.Places[0].DetailsErr != nil {
		t.Fatal("ошибка описания должна быть только у второго места")
	}
}

func TestLoadReportRespectsParallelLimit(t *testing.T) {
	fp := &fakePlaces{count: 8, detailsDelay: 30 * time.Millisecond}
	s := New(fakeGeo{}, fakeWeather{}, fp, Options{ParallelDetails: 2})

	<-s.LoadReport(context.Background(), model.Location{})

	if fp.maxSeen > 2 {
		t.Fatalf("одновременно шло %d запросов, лимит 2", fp.maxSeen)
	}
}

func (f fakeGeo) SearchAsync(ctx context.Context, q string, done func([]model.Location, error)) {
	go func() { done(f.Search(ctx, q)) }()
}
func (f fakeWeather) CurrentAsync(ctx context.Context, lat, lon float64, done func(model.Weather, error)) {
	go func() { done(f.Current(ctx, lat, lon)) }()
}
func (f *fakePlaces) NearbyAsync(ctx context.Context, lat, lon float64, r, l int, done func([]model.PlaceBrief, error)) {
	go func() { done(f.Nearby(ctx, lat, lon, r, l)) }()
}
func (f *fakePlaces) DetailsAsync(ctx context.Context, xid string, done func(model.PlaceDetails, error)) {
	go func() { done(f.Details(ctx, xid)) }()
}
