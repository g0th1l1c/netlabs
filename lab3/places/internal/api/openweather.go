package api

import (
	"context"
	"net/url"
	"strconv"

	"places/internal/httpclient"
	"places/internal/model"
)

// OpenWeather — клиент погоды
// https://openweathermap.org/current
type OpenWeather struct {
	HTTP    *httpclient.Client
	Key     string
	BaseURL string // по умолчанию https://api.openweathermap.org/data/2.5
}

// NewOpenWeather создаёт клиент
func NewOpenWeather(h *httpclient.Client, key string) *OpenWeather {
	return &OpenWeather{HTTP: h, Key: key, BaseURL: "https://api.openweathermap.org/data/2.5"}
}

type weatherResponse struct {
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Pressure  float64 `json:"pressure"`
		Humidity  float64 `json:"humidity"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
}

// Current получает погоду по координатам
func (o *OpenWeather) Current(ctx context.Context, lat, lon float64) (model.Weather, error) {
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 6, 64))
	q.Set("units", "metric")
	q.Set("lang", "ru")
	q.Set("appid", o.Key)

	var resp weatherResponse
	if err := o.HTTP.GetJSON(ctx, o.BaseURL+"/weather?"+q.Encode(), &resp); err != nil {
		return model.Weather{}, err
	}

	w := model.Weather{
		TempC:       resp.Main.Temp,
		FeelsLikeC:  resp.Main.FeelsLike,
		Humidity:    resp.Main.Humidity,
		PressureHPa: resp.Main.Pressure,
		WindMS:      resp.Wind.Speed,
	}
	if len(resp.Weather) > 0 {
		w.Description = resp.Weather[0].Description
	}
	return w, nil
}
