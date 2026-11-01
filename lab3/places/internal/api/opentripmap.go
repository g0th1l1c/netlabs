package api

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"places/internal/httpclient"
	"places/internal/model"
)

// OpenTripMap — клиент интересных мест (методы [3] и [4] из задания).
// https://opentripmap.io/docs
type OpenTripMap struct {
	HTTP    *httpclient.Client
	Key     string
	BaseURL string // по умолчанию https://api.opentripmap.com/0.1/ru
}

// NewOpenTripMap создаёт клиент с адресом по умолчанию.
func NewOpenTripMap(h *httpclient.Client, key string) *OpenTripMap {
	return &OpenTripMap{HTTP: h, Key: key, BaseURL: "https://api.opentripmap.com/0.1/ru"}
}

type radiusItem struct {
	Xid   string  `json:"xid"`
	Name  string  `json:"name"`
	Kinds string  `json:"kinds"`
	Dist  float64 `json:"dist"`
	Rate  float64 `json:"rate"`
}

// Nearby возвращает до limit именованных мест в радиусе radiusM метров,
// отсортированных по расстоянию.
func (o *OpenTripMap) Nearby(ctx context.Context, lat, lon float64, radiusM, limit int) ([]model.PlaceBrief, error) {
	q := url.Values{}
	q.Set("radius", strconv.Itoa(radiusM))
	q.Set("lat", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 6, 64))
	// Просим с запасом: сервер отдаёт и объекты без названия, их мы отбросим.
	q.Set("limit", strconv.Itoa(limit*3))
	q.Set("format", "json")
	q.Set("apikey", o.Key)

	var items []radiusItem
	if err := o.HTTP.GetJSON(ctx, o.BaseURL+"/places/radius?"+q.Encode(), &items); err != nil {
		return nil, err
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Dist < items[j].Dist })

	places := make([]model.PlaceBrief, 0, limit)
	for _, it := range items {
		if strings.TrimSpace(it.Name) == "" || it.Xid == "" {
			continue
		}
		places = append(places, model.PlaceBrief{
			Xid:      it.Xid,
			Name:     it.Name,
			Kinds:    it.Kinds,
			Distance: it.Dist,
			Rate:     it.Rate,
		})
		if len(places) == limit {
			break
		}
	}
	return places, nil
}

type detailsResponse struct {
	URL     string `json:"url"`
	Preview struct {
		Source string `json:"source"`
	} `json:"preview"`
	Address struct {
		City        string `json:"city"`
		Road        string `json:"road"`
		HouseNumber string `json:"house_number"`
	} `json:"address"`
	WikipediaExtracts struct {
		Text string `json:"text"`
	} `json:"wikipedia_extracts"`
	Info struct {
		Descr string `json:"descr"`
	} `json:"info"`
}

// Details возвращает описание места по его xid.
func (o *OpenTripMap) Details(ctx context.Context, xid string) (model.PlaceDetails, error) {
	q := url.Values{}
	q.Set("apikey", o.Key)

	var resp detailsResponse
	if err := o.HTTP.GetJSON(ctx, o.BaseURL+"/places/xid/"+url.PathEscape(xid)+"?"+q.Encode(), &resp); err != nil {
		return model.PlaceDetails{}, err
	}

	// Описание берём из Википедии, а если его нет — из собственного поля сервиса.
	descr := strings.TrimSpace(resp.WikipediaExtracts.Text)
	if descr == "" {
		descr = strings.TrimSpace(resp.Info.Descr)
	}

	var addr []string
	for _, part := range []string{resp.Address.City, resp.Address.Road, resp.Address.HouseNumber} {
		if part != "" {
			addr = append(addr, part)
		}
	}

	return model.PlaceDetails{
		Description: descr,
		Address:     strings.Join(addr, ", "),
		URL:         resp.URL,
		Image:       resp.Preview.Source,
	}, nil
}
