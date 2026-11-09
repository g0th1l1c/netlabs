// Package httpclient выполняет HTTP-запросы и читает JSON
package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rezmoss/axios4go"
)

// Client выполняет GET-запросы
type Client struct {
	timeout time.Duration
}

// New создаёт клиент с таймаутом
func New(timeout time.Duration) *Client {
	return &Client{timeout: timeout}
}

// GetJSON выполняет запрос и разбирает JSON-ответ
func (c *Client) GetJSON(ctx context.Context, rawURL string, target any) error {
	result := make(chan struct {
		response *axios4go.Response
		err      error
	}, 1)
	options := &axios4go.RequestOptions{
		Timeout: int(c.timeout / time.Millisecond),
		Headers: map[string]string{"Accept": "application/json"},
	}
	axios4go.GetAsync(rawURL, options).
		Then(func(resp *axios4go.Response) {
			result <- struct {
				response *axios4go.Response
				err      error
			}{response: resp}
		}).
		Catch(func(err error) {
			result <- struct {
				response *axios4go.Response
				err      error
			}{err: err}
		})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-result:
		if res.err != nil {
			return cleanError(res.err)
		}
		if res.response.StatusCode < 200 || res.response.StatusCode > 299 {
			body := res.response.Body
			if len(body) > 300 {
				body = body[:300]
			}
			return fmt.Errorf("сервер ответил %s: %s", http.StatusText(res.response.StatusCode), strings.TrimSpace(string(body)))
		}
		if err := json.Unmarshal(res.response.Body, target); err != nil {
			return fmt.Errorf("не удалось разобрать ответ сервера: %w", err)
		}
	}
	return nil
}

// cleanError удаляет URL с ключом из ошибки
func cleanError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
