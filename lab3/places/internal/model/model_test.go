package model

import "testing"

func TestLocationTitle(t *testing.T) {
	l := Location{Name: "Москва", City: "Москва", Country: "Россия", Street: " "}
	if got, want := l.Title(), "Москва, Россия"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
