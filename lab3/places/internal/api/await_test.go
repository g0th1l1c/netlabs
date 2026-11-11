package api

import (
	"context"
	"places/internal/model"
)

// ожидание нужно только тестам, которые проверяют итоговый ответ
func await[T any](start func(func(T, error))) (T, error) {
	type result struct {
		value T
		err   error
	}
	ch := make(chan result, 1)
	start(func(value T, err error) { ch <- result{value, err} })
	r := <-ch
	return r.value, r.err
}
func (g *GraphHopper) Search(ctx context.Context, q string) ([]model.Location, error) {
	return await(func(done func([]model.Location, error)) { g.SearchAsync(ctx, q, done) })
}
func (o *OpenWeather) Current(ctx context.Context, lat, lon float64) (model.Weather, error) {
	return await(func(done func(model.Weather, error)) { o.CurrentAsync(ctx, lat, lon, done) })
}
func (o *OpenTripMap) Nearby(ctx context.Context, lat, lon float64, r, l int) ([]model.PlaceBrief, error) {
	return await(func(done func([]model.PlaceBrief, error)) { o.NearbyAsync(ctx, lat, lon, r, l, done) })
}
func (o *OpenTripMap) Details(ctx context.Context, xid string) (model.PlaceDetails, error) {
	return await(func(done func(model.PlaceDetails, error)) { o.DetailsAsync(ctx, xid, done) })
}
