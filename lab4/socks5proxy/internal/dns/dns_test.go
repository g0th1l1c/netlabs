package dns

import (
	"bytes"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
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
		0xc0, 12, 0, 5, 0, 1, 0, 0, 0, 60, 0, 5, 3, 'a', 'p', 'p', 0,
		3, 'a', 'p', 'p', 0, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 1, 2, 3, 4,
	}
	id, ip, err := parseResponse(msg)
	if err != nil || id != 0xabcd || ip != [4]byte{1, 2, 3, 4} {
		t.Fatalf("id=%x ip=%v err=%v", id, ip, err)
	}
}

func TestResponseAddressMatchesAliasChain(t *testing.T) {
	record := func(owner string, body dnsmessage.ResourceBody) dnsmessage.Resource {
		return dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(owner), Class: dnsmessage.ClassINET},
			Body:   body,
		}
	}
	m := dnsmessage.Message{
		Header:    dnsmessage.Header{Response: true},
		Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("WWW.test.")}},
		Answers: []dnsmessage.Resource{
			record("other.test.", &dnsmessage.AResource{A: [4]byte{9, 9, 9, 9}}),
			record("dst.test.", &dnsmessage.AResource{A: [4]byte{1, 2, 3, 4}}),
			record("mid.test.", &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("DST.test.")}),
			record("www.test.", &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("mid.test.")}),
		},
	}
	if ip, err := responseIP(&m); ip != [4]byte{1, 2, 3, 4} || err != nil {
		t.Fatalf("ip=%v err=%v", ip, err)
	}
	m.Answers = m.Answers[:1]
	if _, err := responseIP(&m); err != ErrNotFound {
		t.Fatalf("unrelated address: %v", err)
	}
	m.Answers = append(m.Answers, record("www.test.", &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("WWW.test.")}))
	if _, err := responseIP(&m); err != ErrFormat {
		t.Fatalf("alias loop: %v", err)
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
