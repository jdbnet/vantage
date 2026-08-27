package httpapi

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jdbnet/vantage/internal/model"
)

const (
	hostPingTimeout = 2 * time.Second
	hostPingMaxIDs  = 128
	hostPingWorkers = 16
)

func tcpOpen(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
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
	res := pingOne(s.d.Store.GetHost, h, hostPingTimeout)
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
	results := pingHosts(s.d.Store.GetHost, ids, hostPingTimeout)
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

func pingOne(lookup hostLookup, h model.Host, timeout time.Duration) hostPing {
	hop, via, ok := firstHop(lookup, h)
	if !ok {
		return hostPing{Up: false, ViaJump: via}
	}
	return hostPing{Up: tcpOpen(hop.Hostname, hop.Port, timeout), ViaJump: via}
}

func pingHosts(lookup hostLookup, ids []string, timeout time.Duration) map[string]hostPing {
	out := make(map[string]hostPing, len(ids))
	if len(ids) == 0 {
		return out
	}
	var mu sync.Mutex
	sem := make(chan struct{}, hostPingWorkers)
	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			h, err := lookup(id)
			var res hostPing
			if err == nil {
				res = pingOne(lookup, h, timeout)
			}
			mu.Lock()
			out[id] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
