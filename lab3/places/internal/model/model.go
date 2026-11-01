// Package model хранит структуры данных приложения
package model

import "strings"

// Location — найденная локация
type Location struct {
	Name    string
	Street  string
	City    string
	State   string
	Country string
	Type    string
	Lat     float64
	Lon     float64
}

// Title собирает название локации в одну строку
func (l Location) Title() string {
	parts := []string{l.Name, l.Street, l.City, l.State, l.Country}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || contains(out, p) {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ", ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Weather — текущая погода
type Weather struct {
	Description string  // "пасмурно"
	TempC       float64 // температура, °C
	FeelsLikeC  float64 // ощущаемая температура, °C
	Humidity    float64 // влажность, %
	PressureHPa float64 // давление, гПа
	WindMS      float64 // скорость ветра, м/с
}

// PlaceBrief — краткие данные о месте
type PlaceBrief struct {
	Xid      string
	Name     string
	Kinds    string  // категории через запятую
	Rate     float64 // рейтинг OpenTripMap, если доступен
	Distance float64 // расстояние до точки поиска, метры
}

// PlaceDetails — подробности о месте
type PlaceDetails struct {
	Description string
	Address     string
	URL         string
	Image       string
}

// Place — место вместе с подробностями
type Place struct {
	PlaceBrief
	PlaceDetails
	DetailsErr error
}

// Report — данные по выбранной локации
type Report struct {
	Location   Location
	Weather    *Weather // nil, если погоду получить не удалось
	WeatherErr error
	Places     []Place
	PlacesErr  error // ошибка получения списка мест
}
