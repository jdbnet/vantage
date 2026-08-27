package syncx

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/store"
)

func StartClient(st *store.Store, boxFn func() *cryptox.Box) func() {
	stop := make(chan struct{})
	go loop(st, boxFn, stop)
	return func() { close(stop) }
}

func loop(st *store.Store, boxFn func() *cryptox.Box, stop <-chan struct{}) {
	var lastPush int64
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			settings, err := st.LoadSettings()
			if err != nil || settings.SyncURL == "" {
				continue
			}
			key, ok, _ := st.SyncAPIKey()
			if !ok || key == "" {
				continue
			}
			box := boxFn()
			if box == nil {
				continue
			}
			base := strings.TrimRight(settings.SyncURL, "/")
			if err := pushLocal(st, box, base, key, &lastPush); err != nil {
				log.Printf("sync push: %v", err)
			}
			if err := pullRemote(st, box, base, key); err != nil {
				log.Printf("sync pull: %v", err)
			}
			connectWS(st, box, base, key, stop)
		}
	}
}

func authHeader(key string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+key)
	return h
}

func pushLocal(st *store.Store, box *cryptox.Box, base, key string, last *int64) error {
	ops, head, err := st.ChangesSince(*last)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		*last = head
		return nil
	}
	out := make([]model.ChangeOp, 0, len(ops))
	for _, op := range ops {
		out = append(out, rewriteOp(box, op, true))
	}
	body, _ := json.Marshal(map[string]any{"ops": out})
	req, err := http.NewRequest(http.MethodPost, base+"/api/sync/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}
	*last = head
	return nil
}

func pullRemote(st *store.Store, box *cryptox.Box, base, key string) error {
	req, err := http.NewRequest(http.MethodGet, base+"/api/sync/changes?since=0", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil
	}
	var body struct {
		Ops []model.ChangeOp `json:"ops"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	for _, op := range body.Ops {
		op = rewriteOp(box, op, false)
		_ = st.ApplyRemoteOp(op)
	}
	return nil
}

func connectWS(st *store.Store, box *cryptox.Box, base, key string, stop <-chan struct{}) {
	u, err := url.Parse(base)
	if err != nil {
		return
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/sync"
	hdr := authHeader(key)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), hdr)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "hello", "since": 0})
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Type string           `json:"type"`
			Ops  []model.ChangeOp `json:"ops"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type == "ops" {
			for _, op := range msg.Ops {
				op = rewriteOp(box, op, false)
				_ = st.ApplyRemoteOp(op)
			}
		}
		_ = conn.WriteJSON(map[string]any{"type": "poll"})
	}
}

func rewriteOp(box *cryptox.Box, op model.ChangeOp, outbound bool) model.ChangeOp {
	if box == nil || (op.Entity != "identity" && op.Entity != "host") {
		return op
	}
	var m map[string]any
	if json.Unmarshal(op.Payload, &m) != nil {
		return op
	}
	if op.Entity == "identity" {
		if outbound {
			if blob, _ := m["encrypted_blob"].(string); blob != "" {
				if plain, err := box.Decrypt(blob); err == nil {
					m["secret"] = json.RawMessage(plain)
					delete(m, "encrypted_blob")
				}
			}
		} else if sec, ok := m["secret"]; ok {
			raw, _ := json.Marshal(sec)
			if blob, err := box.Encrypt(string(raw)); err == nil {
				m["encrypted_blob"] = blob
				delete(m, "secret")
			}
		}
	}
	b, _ := json.Marshal(m)
	op.Payload = b
	return op
}
