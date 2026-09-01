package httpapi

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	if w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return h.Hijack()
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (s *Server) withLogs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 0}
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, rec)
				if sw.status == 0 {
					http.Error(sw, "internal error", http.StatusInternalServerError)
				}
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			d := time.Since(start)
			if !shouldLogRequest(r, status, d) {
				return
			}
			log.Printf("%s %s %d %s from %s", r.Method, logPath(r), status, d.Round(time.Millisecond), reqIP(r))
		}()
		next.ServeHTTP(sw, r)
	})
}

func shouldLogRequest(r *http.Request, status int, d time.Duration) bool {
	if status >= 400 {
		return true
	}
	if d >= 500*time.Millisecond {
		return true
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/ws/") {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		return true
	}
	if strings.HasPrefix(path, "/api/sync") {
		return true
	}
	switch path {
	case "/api/me", "/api/inventory/head", "/api/sync/status", "/api/browse":
		return false
	}
	if strings.HasPrefix(path, "/api/") {
		return true
	}
	return false
}

func logPath(r *http.Request) string {
	q := r.URL.Query()
	q.Del("token")
	enc := q.Encode()
	if enc == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + enc
}

func reqIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
