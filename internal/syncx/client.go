package syncx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/store"
)

type Status struct {
	Enabled       bool    `json:"enabled"`
	Configured    bool    `json:"configured"`
	VaultLocked   bool    `json:"vault_locked"`
	WSConnected   bool    `json:"ws_connected"`
	LastSuccessAt *string `json:"last_success_at"`
	LastError     string  `json:"last_error"`
	LastErrorAt   *string `json:"last_error_at"`
}

type Client struct {
	st      *store.Store
	boxFn   func() *cryptox.Box
	dataDir string

	stop     chan struct{}
	kick     chan struct{}
	fileKick chan struct{}

	mu            sync.Mutex
	stopped       bool
	lastPush      int64
	configured    bool
	vaultLocked   bool
	wsConnected   bool
	lastSuccessAt time.Time
	lastError     string
	lastErrorAt   time.Time

	wsMu sync.Mutex
	ws   *websocket.Conn
}

func StartClient(st *store.Store, boxFn func() *cryptox.Box, dataDir string) *Client {
	c := &Client{
		st:       st,
		boxFn:    boxFn,
		dataDir:  dataDir,
		stop:     make(chan struct{}),
		kick:     make(chan struct{}, 1),
		fileKick: make(chan struct{}, 1),
	}
	go c.loop()
	go c.fileLoop()
	return c
}

func (c *Client) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	close(c.stop)
	c.mu.Unlock()
	c.closeWS()
}

func (c *Client) Kick() {
	c.mu.Lock()
	c.lastPush = 0
	c.mu.Unlock()
	c.closeWS()
	select {
	case c.kick <- struct{}{}:
	default:
	}
	if c.fileKick != nil {
		select {
		case c.fileKick <- struct{}{}:
		default:
		}
	}
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{
		Enabled:     true,
		Configured:  c.configured,
		VaultLocked: c.vaultLocked,
		WSConnected: c.wsConnected,
		LastError:   c.lastError,
	}
	if !c.lastSuccessAt.IsZero() {
		s := c.lastSuccessAt.UTC().Format(time.RFC3339)
		st.LastSuccessAt = &s
	}
	if !c.lastErrorAt.IsZero() {
		s := c.lastErrorAt.UTC().Format(time.RFC3339)
		st.LastErrorAt = &s
	}
	return st
}

func (c *Client) loop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		c.pushPull()
		select {
		case <-c.stop:
			return
		case <-c.kick:
			continue
		default:
		}
		c.connectWS()
		select {
		case <-c.stop:
			return
		case <-c.kick:
			continue
		case <-ticker.C:
		}
	}
}

func (c *Client) pushPull() {
	settings, err := c.st.LoadSettings()
	if err != nil || settings.SyncURL == "" {
		c.setIdle(false, false)
		return
	}
	key, ok, _ := c.st.SyncAPIKey()
	if !ok || key == "" {
		c.setIdle(false, false)
		return
	}
	box := c.boxFn()
	if box == nil {
		c.setIdle(true, true)
		return
	}
	c.setIdle(true, false)
	base := strings.TrimRight(settings.SyncURL, "/")
	c.mu.Lock()
	lastPush := c.lastPush
	c.mu.Unlock()
	if err := pushLocal(c.st, box, base, key, &lastPush); err != nil {
		c.setError(err)
		log.Printf("sync push: %v", err)
	} else {
		c.mu.Lock()
		c.lastPush = lastPush
		c.mu.Unlock()
	}
	if err := pullRemote(c.st, box, base, key); err != nil {
		c.setError(err)
		log.Printf("sync pull: %v", err)
		return
	}
	c.setSuccess()
}

func (c *Client) setIdle(configured, vaultLocked bool) {
	c.mu.Lock()
	c.configured = configured
	c.vaultLocked = vaultLocked
	c.mu.Unlock()
}

func (c *Client) setError(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.lastError = err.Error()
	c.lastErrorAt = time.Now()
	c.mu.Unlock()
}

func (c *Client) setSuccess() {
	c.mu.Lock()
	c.lastSuccessAt = time.Now()
	if !strings.HasPrefix(c.lastError, "shared files:") {
		c.lastError = ""
	}
	c.mu.Unlock()
}

func (c *Client) clearFileError() {
	c.mu.Lock()
	if strings.HasPrefix(c.lastError, "shared files:") {
		c.lastError = ""
	}
	c.mu.Unlock()
}

func (c *Client) closeWS() {
	c.wsMu.Lock()
	conn := c.ws
	c.ws = nil
	c.wsMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	c.mu.Lock()
	c.wsConnected = false
	c.mu.Unlock()
}

func (c *Client) setWS(conn *websocket.Conn) {
	c.wsMu.Lock()
	c.ws = conn
	c.wsMu.Unlock()
	c.mu.Lock()
	c.wsConnected = conn != nil
	c.mu.Unlock()
}

func authHeader(key string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+key)
	return h
}

func httpStatusError(op string, status int) error {
	return fmt.Errorf("%s: HTTP %d", op, status)
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
		return httpStatusError("push", resp.StatusCode)
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
		return httpStatusError("pull", resp.StatusCode)
	}
	var body struct {
		Ops []model.ChangeOp `json:"ops"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	return applyPulledOps(st, box, body.Ops)
}

func applyPulledOps(st *store.Store, box *cryptox.Box, ops []model.ChangeOp) error {
	for _, op := range ops {
		op = rewriteOp(box, op, false)
		if err := st.ApplyRemoteOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) connectWS() {
	settings, err := c.st.LoadSettings()
	if err != nil || settings.SyncURL == "" {
		return
	}
	key, ok, _ := c.st.SyncAPIKey()
	if !ok || key == "" {
		return
	}
	box := c.boxFn()
	if box == nil {
		return
	}
	base := strings.TrimRight(settings.SyncURL, "/")
	u, err := url.Parse(base)
	if err != nil {
		c.setError(err)
		return
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/sync"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), authHeader(key))
	if err != nil {
		return
	}
	c.setWS(conn)
	defer c.closeWS()
	if err := conn.WriteJSON(map[string]any{"type": "hello", "since": 0}); err != nil {
		c.setError(err)
		return
	}
	for {
		select {
		case <-c.stop:
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
			if err := applyPulledOps(c.st, box, msg.Ops); err != nil {
				c.setError(err)
				continue
			}
			if len(msg.Ops) > 0 {
				c.setSuccess()
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
