// Package model содержит простые структуры данных приложения.
// Здесь нет логики и нет зависимостей от других пакетов проекта.
package model

import "strings"

// Location — найденная локация (результат геокодинга).
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

// Title возвращает человекочитаемое описание локации одной строкой,
// например: "Цветной проезд, Москва, Россия".
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

// Weather — текущая погода в точке.
type Weather struct {
	Description string  // "пасмурно"
	TempC       float64 // температура, °C
	FeelsLikeC  float64 // ощущаемая температура, °C
	Humidity    float64 // влажность, %
	PressureHPa float64 // давление, гПа
	WindMS      float64 // скорость ветра, м/с
}

// PlaceBrief — краткие сведения об интересном месте (из списка по радиусу).
type PlaceBrief struct {
	Xid      string
	Name     string
	Kinds    string  // категории через запятую
	Rate     float64 // рейтинг OpenTripMap, если доступен
	Distance float64 // расстояние до точки поиска, метры
}

// PlaceDetails — подробности о месте (запрос по xid).
type PlaceDetails struct {
	Description string
	Address     string
	URL         string
	Image       string
}

// Place — интересное место вместе с описанием.
// Если описание получить не удалось, в DetailsErr лежит причина,
// а остальные поля всё равно заполнены.
type Place struct {
	PlaceBrief
	PlaceDetails
	DetailsErr error
}

// Report — итоговый общий объект: всё, что нашли про выбранную локацию.
// Ошибки хранятся отдельно по каждой части, чтобы сбой одного API
// не лишал пользователя результатов остальных.
type Report struct {
	Location   Location
	Weather    *Weather // nil, если погоду получить не удалось
	WeatherErr error
	Places     []Place
	PlacesErr  error // ошибка получения списка мест
}
