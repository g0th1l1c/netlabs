// содержит клиенты внешних сервисов
package api

import (
	"context"
	"net/url"
	"strconv"

	"places/internal/httpclient"
	"places/internal/model"
)

// https://docs.graphhopper.com/#operation/getGeocode
type GraphHopper struct {
	HTTP    *httpclient.Client
	Key     string
	BaseURL string // по умолчанию https://graphhopper.com/api/1
	Limit   int    // сколько вариантов локаций просить у сервера
}

// NewGraphHopper создаёт клиент
func NewGraphHopper(h *httpclient.Client, key string) *GraphHopper {
	return &GraphHopper{HTTP: h, Key: key, BaseURL: "https://graphhopper.com/api/1", Limit: 7}
}

// нужные поля ответа GraphHopper
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

// Search ищет локации по названию
func (g *GraphHopper) SearchAsync(ctx context.Context, query string, done func([]model.Location, error)) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("locale", "ru")
	q.Set("limit", strconv.Itoa(g.Limit))
	q.Set("key", g.Key)

	var resp geocodeResponse
	g.HTTP.GetJSON(ctx, g.BaseURL+"/geocode?"+q.Encode(), &resp, func(err error) {
		if err != nil {
			done(nil, err)
			return
		}
		parse := func() ([]model.Location, error) {
			locations := make([]model.Location, 0, len(resp.Hits))
			//0 - сколько элементов сейчас находится в срезе,len(resp.Hits) - сколько элементов можно разместить
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
		done(parse())
	})
}
