// Package api содержит клиенты внешних сервисов.
// Каждый клиент умеет одно: собрать URL, сделать запрос и превратить
// ответ сервера в структуры из пакета model.
package api

import (
	"context"
	"net/url"
	"strconv"

	"places/internal/httpclient"
	"places/internal/model"
)

// GraphHopper — клиент геокодинга (метод [1] из задания).
// https://docs.graphhopper.com/#operation/getGeocode
type GraphHopper struct {
	HTTP    *httpclient.Client
	Key     string
	BaseURL string // по умолчанию https://graphhopper.com/api/1
	Limit   int    // сколько вариантов локаций просить у сервера
}

// NewGraphHopper создаёт клиент с адресом по умолчанию.
func NewGraphHopper(h *httpclient.Client, key string) *GraphHopper {
	return &GraphHopper{HTTP: h, Key: key, BaseURL: "https://graphhopper.com/api/1", Limit: 7}
}

// Часть ответа, которая нам нужна. Остальные поля JSON игнорируются.
type geocodeResponse struct {
	Hits []struct {
		Name        string `json:"name"`
		Street      string `json:"street"`
		HouseNumber string `json:"housenumber"`
		City        string `json:"city"`
		State       string `json:"state"`
		Country     string `json:"country"`
		OsmValue    string `json:"osm_value"`
		Point       struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"point"`
	} `json:"hits"`
}

// Search ищет локации по названию.
func (g *GraphHopper) Search(ctx context.Context, query string) ([]model.Location, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("locale", "ru")
	q.Set("limit", strconv.Itoa(g.Limit))
	q.Set("key", g.Key)

	var resp geocodeResponse
	if err := g.HTTP.GetJSON(ctx, g.BaseURL+"/geocode?"+q.Encode(), &resp); err != nil {
		return nil, err
	}

	locations := make([]model.Location, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		street := h.Street
		if street != "" && h.HouseNumber != "" {
			street += ", " + h.HouseNumber
		}
		locations = append(locations, model.Location{
			Name:    h.Name,
			Street:  street,
			City:    h.City,
			State:   h.State,
			Country: h.Country,
			Type:    h.OsmValue,
			Lat:     h.Point.Lat,
			Lon:     h.Point.Lng,
		})
	}
	return locations, nil
}
