package webui

import (
	"context"
	"errors"
	"net/http/httptest"
	"places/internal/model"
	"places/internal/service"
	"strings"
	"testing"
)

type fake struct{}

func (fake) FindLocations(ctx context.Context, q string) <-chan service.LocationsResult {
	ch := make(chan service.LocationsResult, 1)
	ch <- service.LocationsResult{Locations: []model.Location{{Name: "Цветной проезд", Type: "bus_stop"}}}
	return ch
}
func (fake) LoadReport(ctx context.Context, l model.Location) <-chan model.Report {
	ch := make(chan model.Report, 1)
	ch <- model.Report{Location: l, WeatherErr: errors.New("погода недоступна"), Places: []model.Place{{PlaceBrief: model.PlaceBrief{Name: "Музей"}, DetailsErr: errors.New("таймаут")}}}
	return ch
}
func TestPageAndAPI(t *testing.T) {
	h := Handler(fake{})
	for _, tc := range []struct {
		method, path, body, want string
		code                     int
	}{
		{"GET", "/", "", "Что интересного рядом?", 200},
		{"GET", "/api/search?q=цветной", "", "bus_stop", 200},
		{"POST", "/api/report", `{"Name":"Цветной проезд","Lat":55,"Lon":37}`, "погода недоступна", 200},
		{"POST", "/api/report", `{"Lat":999}`, "неверная локация", 400},
		{"GET", "/api/report", "", "метод не поддерживается", 405},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		h.ServeHTTP(w, r)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
