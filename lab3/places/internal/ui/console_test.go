package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"places/internal/model"
	"places/internal/service"
)

type fakeGeo struct{}

func (fakeGeo) Search(ctx context.Context, q string) ([]model.Location, error) {
	return []model.Location{
		{Name: "Цветной проезд", City: "Москва", Country: "Россия", Lat: 1, Lon: 2},
		{Name: "Цветной бульвар", City: "Москва", Country: "Россия", Lat: 3, Lon: 4},
	}, nil
}

type fakeWeather struct{}

func (fakeWeather) Current(ctx context.Context, lat, lon float64) (model.Weather, error) {
	return model.Weather{Description: "пасмурно", TempC: 7.2, FeelsLikeC: 5, Humidity: 80, PressureHPa: 1012, WindMS: 3}, nil
}

type fakePlaces struct{}

func (fakePlaces) Nearby(ctx context.Context, lat, lon float64, r, l int) ([]model.PlaceBrief, error) {
	return []model.PlaceBrief{{Xid: "1", Name: "Цирк Никулина", Kinds: "cultural,theatres_and_entertainments", Distance: 120},
		{Xid: "2", Name: "Памятник", Distance: 300}}, nil
}

func (fakePlaces) Details(ctx context.Context, xid string) (model.PlaceDetails, error) {
	if xid == "2" {
		return model.PlaceDetails{}, errors.New("таймаут")
	}
	return model.PlaceDetails{Description: "Старейший цирк Москвы.", Address: "Москва, Цветной бульвар", URL: "http://x"}, nil
}

func run(t *testing.T, input string) string {
	t.Helper()
	svc := service.New(fakeGeo{}, fakeWeather{}, fakePlaces{}, service.Options{PlacesLimit: 5, ParallelDetails: 2})
	var out bytes.Buffer
	NewConsole(svc, strings.NewReader(input), &out).Run(context.Background())
	return out.String()
}

func TestFullScenario(t *testing.T) {
	out := run(t, "цветной\n1\nq\n")
	for _, want := range []string{"Найдено:", "1. Цветной проезд", "ПОГОДА", "Пасмурно, 7.2", "Цирк Никулина", "Старейший цирк", "Описание недоступно: таймаут"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q\n%s", want, out)
		}
	}
	t.Log("\n" + out)
}

func TestInvalidChoiceThenValid(t *testing.T) {
	out := run(t, "цветной\n99\nабв\n2\nq\n")
	if !strings.Contains(out, "Введите число от 1 до 2") || !strings.Contains(out, "ПОГОДА") {
		t.Fatalf("неожиданный вывод:\n%s", out)
	}
}

func TestEOFExitsCleanly(t *testing.T) {
	run(t, "") // не должно зависнуть
}

func TestShorten(t *testing.T) {
	if got := shorten("привет мир", 6); got != "привет..." {
		t.Fatalf("got %q", got)
	}
}
