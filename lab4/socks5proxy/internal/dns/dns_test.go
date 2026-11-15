package dns

import (
	"bytes"
	"testing"
)

func TestBuildQuery(t *testing.T) {
	msg, err := buildQuery(0x1234, "example.com.")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0, 1, 0, 1,
	}
	if !bytes.Equal(msg, want) {
		t.Fatalf("получено %v, ожидалось %v", msg, want)
	}
}

func TestBuildQueryBadName(t *testing.T) {
	for _, name := range []string{"", "a..b", string(make([]byte, 64)) + ".com"} {
		if _, err := buildQuery(1, name); err != ErrBadName {
			t.Errorf("%q: ожидалась ErrBadName, получено %v", name, err)
		}
	}
}

func TestParseResponseCNAMEThenA(t *testing.T) {
	msg := []byte{
		0xab, 0xcd, 0x81, 0x80, 0, 1, 0, 2, 0, 0, 0, 0,
		3, 'w', 'w', 'w', 0, 0, 1, 0, 1, // вопрос
		0xc0, 12, 0, 5, 0, 1, 0, 0, 0, 60, 0, 2, 0xc0, 12, // cname
		0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 1, 2, 3, 4, // a
	}
	id, ip, err := parseResponse(msg)
	if err != nil || id != 0xabcd || ip != [4]byte{1, 2, 3, 4} {
		t.Fatalf("id=%x ip=%v err=%v", id, ip, err)
	}
}

func TestParseResponseErrors(t *testing.T) {
	nx := []byte{0, 7, 0x81, 0x83, 0, 0, 0, 0, 0, 0, 0, 0}
	if id, _, err := parseResponse(nx); id != 7 || err != ErrNotFound {
		t.Errorf("nxdomain: id=%d err=%v", id, err)
	}
	if _, _, err := parseResponse([]byte{1, 2, 3}); err != ErrFormat {
		t.Errorf("короткое сообщение: %v", err)
	}
	trunc := []byte{0, 1, 0x81, 0x80, 0, 0, 0, 1, 0, 0, 0, 0, 0xc0}
	if _, _, err := parseResponse(trunc); err != ErrFormat {
		t.Errorf("обрезанное сообщение: %v", err)
	}
}
