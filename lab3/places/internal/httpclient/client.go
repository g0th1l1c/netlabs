package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezmoss/axios4go"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Client struct{ timeout time.Duration }

func New(timeout time.Duration) *Client { return &Client{timeout: timeout} }

// запускает запрос и вызывает done после разбора ответа
func (c *Client) GetJSON(ctx context.Context, rawURL string, target any, done func(error)) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout) //создаем дочерний контекст который отменится с таймаутом или если родитель отменится
	var once sync.Once                                 //чтобы функция выполнилась один раз
	finish := func(err error) { once.Do(func() { cancel(); done(err) }) }
	options := &axios4go.RequestOptions{
		MaxContentLength: 10 * 1024 * 1024, // до 10 МиБ на JSON-ответ
		Timeout:          int(c.timeout / time.Millisecond),
		Headers:          map[string]string{"Accept": "application/json"},
		ValidateStatus:   func(int) bool { return true },
		InterceptorOptions: axios4go.InterceptorOptions{ //привязываем к http зпросу контекст
			RequestInterceptors: []func(*http.Request) error{func(req *http.Request) error {
				*req = *req.WithContext(ctx)
				return ctx.Err()
			}},
		},
	}
	axios4go.GetAsync(rawURL, options).Then(
		func(resp *axios4go.Response) {
			if err := ctx.Err(); err != nil {
				finish(err)
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				finish(fmt.Errorf("сервер ответил %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
				return
			}
			if err := json.Unmarshal(resp.Body, target); err != nil {
				finish(fmt.Errorf("не удалось разобрать ответ сервера: %w", err))
				return
			}
			finish(nil)
		}).Catch(func(err error) {
		if ctx.Err() != nil {
			finish(ctx.Err())
			return
		}
		finish(cleanError(err))
	})
}
func cleanError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
