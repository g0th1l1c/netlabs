// Package httpclient — тонкая обёртка над net/http для получения JSON.
// Все API-клиенты проекта ходят в сеть только через неё.
package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client выполняет GET-запросы и разбирает JSON-ответы.
// Безопасен для одновременного использования из нескольких горутин.
type Client struct {
	http *http.Client
}

// New создаёт клиент с таймаутом на каждый запрос.
func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

// GetJSON делает GET на rawURL и раскладывает JSON-ответ в target.
// Запрос отменяется, если отменён ctx.
func (c *Client) GetJSON(ctx context.Context, rawURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return cleanError(err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return cleanError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("сервер ответил %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("не удалось разобрать ответ сервера: %w", err)
	}
	return nil
}

// cleanError убирает URL из текста ошибки: в нём лежит API-ключ,
// а ошибки мы выводим пользователю и можем записать в лог.
func cleanError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
