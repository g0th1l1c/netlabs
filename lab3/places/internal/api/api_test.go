package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"places/internal/httpclient"
)

func newServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGraphHopperSearch(t *testing.T) {
	srv := newServer(t, `{"hits":[{"name":"Цветной проезд","city":"Москва","country":"Россия",
		"street":"Цветной б-р","housenumber":"1","point":{"lat":55.77,"lng":37.62}}]}`, 200)
	g := NewGraphHopper(httpclient.New(time.Second), "k")
	g.BaseURL = srv.URL

	locs, err := g.Search(context.Background(), "цветной")
	if err != nil || len(locs) != 1 {
		t.Fatalf("err=%v locs=%v", err, locs)
	}
	if locs[0].Lat != 55.77 || locs[0].Lon != 37.62 || locs[0].Street != "Цветной б-р, 1" {
		t.Fatalf("неверный разбор: %+v", locs[0])
	}
}

func TestOpenWeatherCurrent(t *testing.T) {
	srv := newServer(t, `{"weather":[{"description":"пасмурно"}],
		"main":{"temp":5.5,"feels_like":3,"pressure":1010,"humidity":80},"wind":{"speed":4}}`, 200)
	o := NewOpenWeather(httpclient.New(time.Second), "k")
	o.BaseURL = srv.URL

	w, err := o.Current(context.Background(), 1, 2)
	if err != nil || w.Description != "пасмурно" || w.TempC != 5.5 || w.WindMS != 4 {
		t.Fatalf("err=%v w=%+v", err, w)
	}
}

func TestOpenTripMapNearbyFiltersAndSorts(t *testing.T) {
	srv := newServer(t, `[
		{"xid":"A","name":"Далеко","dist":500,"kinds":"museums"},
		{"xid":"B","name":"","dist":10,"kinds":"x"},
		{"xid":"C","name":"Близко","dist":50,"kinds":"churches"},
		{"xid":"D","name":"Ещё","dist":900,"kinds":"y"}]`, 200)
	o := NewOpenTripMap(httpclient.New(time.Second), "k")
	o.BaseURL = srv.URL

	places, err := o.Nearby(context.Background(), 1, 2, 1000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 2 || places[0].Xid != "C" || places[1].Xid != "A" {
		t.Fatalf("неверный результат: %+v", places)
	}
}

func TestOpenTripMapDetailsPrefersWikipedia(t *testing.T) {
	srv := newServer(t, `{"url":"http://x","address":{"city":"Москва","road":"Тверская"},
		"wikipedia_extracts":{"text":"Из википедии"},"info":{"descr":"Своё"}}`, 200)
	o := NewOpenTripMap(httpclient.New(time.Second), "k")
	o.BaseURL = srv.URL

	d, err := o.Details(context.Background(), "A")
	if err != nil || d.Description != "Из википедии" || d.Address != "Москва, Тверская" {
		t.Fatalf("err=%v d=%+v", err, d)
	}
}

func TestHTTPErrorDoesNotLeakKey(t *testing.T) {
	srv := newServer(t, `{"message":"Invalid API key"}`, 401)
	o := NewOpenWeather(httpclient.New(time.Second), "SECRETKEY")
	o.BaseURL = srv.URL

	_, err := o.Current(context.Background(), 1, 2)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("ожидалась ошибка 401, получено %v", err)
	}
	if strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatal("ключ API попал в текст ошибки")
	}
}

func TestNetworkErrorDoesNotLeakKey(t *testing.T) {
	o := NewOpenWeather(httpclient.New(time.Second), "SECRETKEY")
	o.BaseURL = "http://127.0.0.1:1" // порт, на котором никто не слушает

	_, err := o.Current(context.Background(), 1, 2)
	if err == nil || strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatalf("ошибка должна быть и не содержать ключ: %v", err)
	}
}
