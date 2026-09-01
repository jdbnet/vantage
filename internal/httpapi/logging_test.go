package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShouldLogRequest(t *testing.T) {
	getMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if shouldLogRequest(getMe, 200, 10*time.Millisecond) {
		t.Fatal("fast me poll should be quiet")
	}
	if !shouldLogRequest(getMe, 500, 10*time.Millisecond) {
		t.Fatal("failed me poll should log")
	}
	postHost := httptest.NewRequest(http.MethodPost, "/api/hosts", nil)
	if !shouldLogRequest(postHost, 201, 10*time.Millisecond) {
		t.Fatal("mutating api should log")
	}
	syncGet := httptest.NewRequest(http.MethodGet, "/api/sync/changes?since=0", nil)
	if !shouldLogRequest(syncGet, 200, 10*time.Millisecond) {
		t.Fatal("sync api should log")
	}
	ws := httptest.NewRequest(http.MethodGet, "/ws/terminal", nil)
	if !shouldLogRequest(ws, 101, 10*time.Millisecond) {
		t.Fatal("websocket should log")
	}
}

func TestLogPathStripsToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws/terminal?host_id=abc&token=secret", nil)
	got := logPath(r)
	if got != "/ws/terminal?host_id=abc" {
		t.Fatalf("got %s", got)
	}
}
