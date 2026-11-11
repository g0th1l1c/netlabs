package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSONResponseLargerThanLibraryDefault(t *testing.T) {
	want := strings.Repeat("описание места ", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"description": want})
	}))
	defer srv.Close()
	var got struct {
		Description string `json:"description"`
	}
	done := make(chan error, 1)
	New(5*time.Second).GetJSON(context.Background(), srv.URL, &got, func(err error) { done <- err })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if got.Description != want {
			t.Fatal("ответ обрезан")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("ответ не получен")
	}
}

func TestRequestReturnsImmediatelyAndCancelsTransport(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, returned := make(chan error, 1), make(chan struct{})
	go func() {
		var target any
		New(5*time.Second).GetJSON(ctx, srv.URL, &target, func(err error) { done <- err })
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("вызов блокируется")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("запрос не дошёл до сервера")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ошибка отмены: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("нет результата отмены")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP-запрос не отменён")
	}
}
