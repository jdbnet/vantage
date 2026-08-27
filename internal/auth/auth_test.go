package auth

import (
	"net/http/httptest"
	"testing"
)

func TestIssueParseRoundTrip(t *testing.T) {
	j := NewJar([]byte("0123456789abcdef0123456789abcdef"), 0, false)
	rec := httptest.NewRecorder()
	tok, err := j.Issue(rec, "jamie")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("expected session token")
	}
	user, err := j.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if user != "jamie" {
		t.Fatalf("got %q", user)
	}
	req := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	got, err := j.User(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "jamie" {
		t.Fatalf("cookie user %q", got)
	}
}

func TestParseRejectsTamper(t *testing.T) {
	j := NewJar([]byte("0123456789abcdef0123456789abcdef"), 0, false)
	rec := httptest.NewRecorder()
	tok, err := j.Issue(rec, "jamie")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse(tok + "x"); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := j.Parse("not-a-token"); err == nil {
		t.Fatal("expected reject")
	}
}
