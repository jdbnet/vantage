package httpapi

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/jdbnet/vantage/internal/model"
)

const (
	hostPingTimeout = 800 * time.Millisecond
	hostPingMaxIDs  = 32
)

func hostPingWorkers() int {
	if runtime.GOOS == "windows" {
		return 4
	}
	return 8
}

func tcpOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	d := net.Dialer{
		Timeout:       timeout,
		FallbackDelay: -1,
		Resolver:      &net.Resolver{PreferGo: true},
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (s *Server) handlePingHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.d.Store.GetHost(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	res := pingOne(r.Context(), s.d.Store.GetHost, h, hostPingTimeout)
	writeJSON(w, 200, map[string]any{"up": res.Up, "via_jump": res.ViaJump})
}

func (s *Server) handlePingHosts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	ids := body.IDs
	if len(ids) > hostPingMaxIDs {
		ids = ids[:hostPingMaxIDs]
	}
	results := pingHosts(r.Context(), s.d.Store.GetHost, ids, hostPingTimeout)
	up := make(map[string]bool, len(results))
	via := make(map[string]bool, len(results))
	for id, res := range results {
		up[id] = res.Up
		via[id] = res.ViaJump
	}
	writeJSON(w, 200, map[string]any{"up": up, "via_jump": via})
}

type hostLookup func(id string) (model.Host, error)

type hostPing struct {
	Up      bool
	ViaJump bool
}

func firstHop(lookup hostLookup, h model.Host) (model.Host, bool, bool) {
	seen := map[string]struct{}{}
	cur := h
	via := false
	for cur.JumpHostID != nil && *cur.JumpHostID != "" {
		if _, ok := seen[cur.ID]; ok {
			return cur, true, false
		}
		seen[cur.ID] = struct{}{}
		parent, err := lookup(*cur.JumpHostID)
		if err != nil {
			return cur, true, false
		}
		cur = parent
		via = true
	}
	return cur, via, true
}

func pingOne(ctx context.Context, lookup hostLookup, h model.Host, timeout time.Duration) hostPing {
	hop, via, ok := firstHop(lookup, h)
	if !ok {
		return hostPing{Up: false, ViaJump: via}
	}
	return hostPing{Up: tcpOpen(ctx, hop.Hostname, hop.Port, timeout), ViaJump: via}
}

func pingHosts(ctx context.Context, lookup hostLookup, ids []string, timeout time.Duration) map[string]hostPing {
	out := make(map[string]hostPing, len(ids))
	if len(ids) == 0 {
		return out
	}
	workers := hostPingWorkers()
	if workers > len(ids) {
		workers = len(ids)
	}
	jobs := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					return
				}
				h, err := lookup(id)
				var res hostPing
				if err == nil {
					res = pingOne(ctx, lookup, h, timeout)
				}
				mu.Lock()
				out[id] = res
				mu.Unlock()
			}
		}()
	}
send:
	for _, id := range ids {
		select {
		case <-ctx.Done():
			break send
		case jobs <- id:
		}
	}
	close(jobs)
	wg.Wait()
	return out
}
