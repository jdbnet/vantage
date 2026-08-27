package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/guacx"
	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/sshx"
	"github.com/jdbnet/vantage/internal/store"
	"github.com/wwt/guac"
)

func (s *Server) wsAuth(r *http.Request, scope string) bool {
	p := s.resolveAuth(r)
	if p == nil {
		return false
	}
	if p.session {
		return true
	}
	return store.HasScope(p.scopes, scope)
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if !s.wsAuth(r, "terminal:connect") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		http.Error(w, "host_id required", http.StatusBadRequest)
		return
	}
	box := s.d.Box()
	if box == nil {
		http.Error(w, "vault locked", http.StatusServiceUnavailable)
		return
	}
	conn, err := s.upgr.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var wsMu sync.Mutex
	writeTerm := func(p []byte) {
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		_ = conn.WriteMessage(websocket.BinaryMessage, p)
	}
	writeJSONLocked := func(v any) {
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		_ = conn.WriteJSON(v)
	}

	type dimState struct {
		mu         sync.Mutex
		cols, rows int
		sess       *sshx.Session
	}
	dims := &dimState{cols: 120, rows: 40}

	lines := make(chan string, 8)
	hostKeys := make(chan sshx.HostKeyDecision, 1)
	rawIn := make(chan []byte, 64)
	authDone := make(chan struct{})
	wsErr := make(chan error, 1)
	var echoAuth atomic.Bool
	echoAuth.Store(true)

	applyResize := func(cols, rows int) {
		dims.mu.Lock()
		defer dims.mu.Unlock()
		if cols > 0 {
			dims.cols = cols
		}
		if rows > 0 {
			dims.rows = rows
		}
		if dims.sess != nil {
			sshx.Resize(dims.sess, dims.cols, dims.rows)
		}
	}

	go func() {
		var line []byte
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				select {
				case wsErr <- err:
				default:
				}
				return
			}
			if len(data) > 0 && data[0] == '{' {
				var msg struct {
					Type    string `json:"type"`
					Cols    int    `json:"cols"`
					Rows    int    `json:"rows"`
					Accept  bool   `json:"accept"`
					Replace bool   `json:"replace"`
				}
				if json.Unmarshal(data, &msg) == nil {
					switch msg.Type {
					case "ping":
						writeJSONLocked(map[string]any{"type": "pong"})
						continue
					case "resize":
						applyResize(msg.Cols, msg.Rows)
						continue
					case "hostkey-reply":
						select {
						case hostKeys <- sshx.HostKeyDecision{Accept: msg.Accept, Replace: msg.Replace}:
						default:
						}
						continue
					}
				}
			}
			select {
			case <-authDone:
				select {
				case rawIn <- data:
				case <-wsErr:
					return
				}
			default:
				for _, b := range data {
					switch b {
					case '\r', '\n':
						select {
						case lines <- string(line):
						case <-authDone:
						}
						line = nil
					case 0x03:
						select {
						case wsErr <- fmt.Errorf("authentication cancelled"):
						default:
						}
						return
					case 0x7f, 0x08:
						if len(line) > 0 {
							line = line[:len(line)-1]
							if echoAuth.Load() {
								writeTerm([]byte{0x08, ' ', 0x08})
							}
						}
					default:
						if b >= 32 {
							line = append(line, b)
							if echoAuth.Load() {
								writeTerm([]byte{b})
							}
						}
					}
				}
			}
		}
	}()

	prompt := &sshx.Prompt{
		Output: func(text string) {
			writeTerm([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
		},
		Line: func(echo bool) (string, error) {
			echoAuth.Store(echo)
			defer echoAuth.Store(true)
			select {
			case line := <-lines:
				return line, nil
			case err := <-wsErr:
				if err == nil {
					err = io.EOF
				}
				return "", err
			case <-time.After(2 * time.Minute):
				return "", fmt.Errorf("timed out waiting for authentication response")
			}
		},
		HostKey: func(info sshx.HostKeyInfo) (sshx.HostKeyDecision, error) {
			writeJSONLocked(map[string]any{
				"type":        "hostkey",
				"hostname":    info.Hostname,
				"port":        info.Port,
				"fingerprint": info.Fingerprint,
				"key_type":    info.KeyType,
				"status":      info.Status,
				"previous":    info.Previous,
			})
			select {
			case dec := <-hostKeys:
				return dec, nil
			case err := <-wsErr:
				if err == nil {
					err = io.EOF
				}
				return sshx.HostKeyDecision{}, err
			case <-time.After(5 * time.Minute):
				return sshx.HostKeyDecision{}, fmt.Errorf("timed out waiting for host key confirmation")
			}
		},
	}

	dims.mu.Lock()
	startCols, startRows := dims.cols, dims.rows
	dims.mu.Unlock()
	sess, err := sshx.Connect(s.d.Store, box, hostID, startCols, startRows, prompt)
	if err != nil {
		writeTerm([]byte(err.Error() + "\r\n"))
		writeJSONLocked(map[string]any{"type": "error", "error": err.Error()})
		return
	}
	dims.mu.Lock()
	dims.sess = sess
	sshx.Resize(sess, dims.cols, dims.rows)
	dims.mu.Unlock()
	close(authDone)
	sess.ID = idgen.New()
	host, _ := s.d.Store.GetHost(hostID)
	if st, _ := s.d.Store.LoadSettings(); st.AuditLogEnabled {
		if aid, err := s.d.Store.InsertAudit(host); err == nil {
			sess.AuditID = aid
		}
	}
	_ = s.d.Store.TouchHostConnected(hostID)
	s.d.Conns.Put(sess)
	defer func() {
		s.d.Conns.Pop(sess.ID)
		sess.Close()
		if sess.AuditID != 0 {
			_ = s.d.Store.FinishAudit(sess.AuditID, sess.Started)
		}
	}()

	writeJSONLocked(map[string]any{"type": "ready", "conn_id": sess.ID, "label": sess.Label})

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Stdout.Read(buf)
			if n > 0 {
				writeTerm(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				writeJSONLocked(map[string]any{"type": "keepalive"})
			}
		}
	}()

	for {
		select {
		case <-wsErr:
			return
		case <-done:
			return
		case data := <-rawIn:
			if _, err := sess.Stdin.Write(data); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleGuacWS(w http.ResponseWriter, r *http.Request) {
	if !s.wsAuth(r, "terminal:connect") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hdr := http.Header{}
	if p := r.Header.Get("Sec-Websocket-Protocol"); p != "" {
		hdr.Set("Sec-Websocket-Protocol", p)
	}
	conn, err := s.upgr.Upgrade(w, r, hdr)
	if err != nil {
		return
	}
	defer conn.Close()

	sendErr := func(msg string) {
		_ = conn.WriteMessage(websocket.TextMessage, guacx.ErrorInstruction(msg))
	}

	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		sendErr("host_id required")
		return
	}
	box := s.d.Box()
	if box == nil {
		sendErr("vault locked")
		return
	}
	st, _ := s.d.Store.LoadSettings()
	if strings.TrimSpace(st.GuacdAddr) == "" {
		sendErr("guacd address is not configured")
		return
	}
	rec, creds, err := sshx.ResolveCreds(s.d.Store, box, hostID)
	if err != nil {
		sendErr(err.Error())
		return
	}
	proto := string(rec.Host.Protocol)
	if proto != "vnc" && proto != "rdp" {
		sendErr("host is not vnc/rdp")
		return
	}
	width := st.DisplayWidth
	height := st.DisplayHeight
	if n, _ := strconvAtoi(r.URL.Query().Get("width")); n >= 320 && n <= 7680 {
		width = n
	}
	if n, _ := strconvAtoi(r.URL.Query().Get("height")); n >= 240 && n <= 4320 {
		height = n
	}
	params := guacx.Params{
		GuacdAddr:  st.GuacdAddr,
		Protocol:   proto,
		Hostname:   rec.Host.Hostname,
		Port:       rec.Host.Port,
		Username:   creds.User,
		Password:   creds.Password,
		Domain:     creds.Domain,
		Width:      width,
		Height:     height,
		ColorDepth: st.DisplayColorDepth,
	}
	if proto == "rdp" {
		driveRoot := s.sharedFilesDir(st)
		drivePath := filepath.Join(driveRoot, hostID)
		if err := os.MkdirAll(drivePath, 0o700); err != nil {
			sendErr("shared files directory: " + err.Error())
			return
		}
		abs, err := filepath.Abs(drivePath)
		if err != nil {
			sendErr("shared files directory: " + err.Error())
			return
		}
		params.EnableDrive = true
		params.DrivePath = abs
		params.DriveName = "Vantage"
	}
	tunnel, err := guacx.Open(params)
	if err != nil {
		sendErr(err.Error())
		return
	}
	defer tunnel.Close()
	_ = s.d.Store.TouchHostConnected(hostID)
	if st.AuditLogEnabled {
		h, _ := s.d.Store.GetHost(hostID)
		if aid, err := s.d.Store.InsertAudit(h); err == nil {
			defer func() { _ = s.d.Store.FinishAudit(aid, time.Now()) }()
		}
	}
	pipeGuac(conn, tunnel)
}

func pipeGuac(ws *websocket.Conn, tunnel guac.Tunnel) {
	writer := tunnel.AcquireWriter()
	reader := tunnel.AcquireReader()
	defer tunnel.ReleaseWriter()
	defer tunnel.ReleaseReader()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 0, guac.MaxGuacMessage*2)
		for {
			ins, err := reader.ReadSome()
			if err != nil {
				if err != io.EOF {
					_ = ws.WriteMessage(websocket.TextMessage, guacx.ErrorInstruction(err.Error()))
				}
				return
			}
			buf = append(buf[:0], ins...)
			if err := ws.WriteMessage(websocket.TextMessage, buf); err != nil {
				return
			}
		}
	}()
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if _, err := writer.Write(data); err != nil {
			return
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

func (s *Server) handleSyncSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.d.Store.Snapshot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rewriteSnapshotSecrets(snap, true)
	writeJSON(w, 200, snap)
}

func (s *Server) handleSyncChanges(w http.ResponseWriter, r *http.Request) {
	since, _ := strconvAtoi(r.URL.Query().Get("since"))
	ops, head, err := s.d.Store.ChangesSince(int64(since))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]model.ChangeOp, 0, len(ops))
	for _, op := range ops {
		out = append(out, s.rewriteOpSecrets(op, true))
	}
	writeJSON(w, 200, map[string]any{"ops": out, "head": head})
}

func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ops []model.ChangeOp `json:"ops"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	for _, op := range body.Ops {
		op = s.rewriteOpSecrets(op, false)
		if err := s.d.Store.ApplyRemoteOp(op); err != nil {
			log.Printf("apply op: %v", err)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSyncWS(w http.ResponseWriter, r *http.Request) {
	if !s.wsAuth(r, "sync") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgr.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var last int64
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Type  string           `json:"type"`
			Since int64            `json:"since"`
			Ops   []model.ChangeOp `json:"ops"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "hello":
			ops, head, _ := s.d.Store.ChangesSince(msg.Since)
			rewritten := make([]model.ChangeOp, 0, len(ops))
			for _, op := range ops {
				rewritten = append(rewritten, s.rewriteOpSecrets(op, true))
			}
			_ = conn.WriteJSON(map[string]any{"type": "ops", "ops": rewritten, "head": head})
			last = head
		case "push":
			for _, op := range msg.Ops {
				op = s.rewriteOpSecrets(op, false)
				_ = s.d.Store.ApplyRemoteOp(op)
			}
			_ = conn.WriteJSON(map[string]any{"type": "ack"})
		case "poll":
			ops, head, _ := s.d.Store.ChangesSince(last)
			if len(ops) > 0 {
				rewritten := make([]model.ChangeOp, 0, len(ops))
				for _, op := range ops {
					rewritten = append(rewritten, s.rewriteOpSecrets(op, true))
				}
				_ = conn.WriteJSON(map[string]any{"type": "ops", "ops": rewritten, "head": head})
				last = head
			}
		}
	}
}

func (s *Server) rewriteSnapshotSecrets(snap map[string]any, outbound bool) {
	box := s.d.Box()
	if box == nil {
		return
	}
	for _, m := range asMapSlice(snap["identities"]) {
		s.rewriteIdentMap(m, box, outbound)
	}
	for _, m := range asMapSlice(snap["hosts"]) {
		s.rewriteHostMap(m, box, outbound)
	}
}

func (s *Server) rewriteIdentMap(m map[string]any, box *cryptox.Box, outbound bool) {
	if m == nil {
		return
	}
	if outbound {
		if blob, _ := m["encrypted_blob"].(string); blob != "" {
			if plain, err := box.Decrypt(blob); err == nil {
				m["secret"] = json.RawMessage(plain)
				delete(m, "encrypted_blob")
			}
		}
		if p, ok := m["encrypted_key_passphrase"].(*string); ok && p != nil {
			if plain, err := box.Decrypt(*p); err == nil {
				m["key_passphrase"] = plain
				delete(m, "encrypted_key_passphrase")
			}
		}
		if p, ok := m["encrypted_key_passphrase"].(string); ok && p != "" {
			if plain, err := box.Decrypt(p); err == nil {
				m["key_passphrase"] = plain
				delete(m, "encrypted_key_passphrase")
			}
		}
		return
	}
	if sec, ok := m["secret"]; ok {
		raw, _ := json.Marshal(sec)
		if blob, err := box.Encrypt(string(raw)); err == nil {
			m["encrypted_blob"] = blob
			delete(m, "secret")
		}
	}
	if kp, _ := m["key_passphrase"].(string); kp != "" {
		if enc, err := box.Encrypt(kp); err == nil {
			m["encrypted_key_passphrase"] = enc
			delete(m, "key_passphrase")
		}
	}
}

func (s *Server) rewriteHostMap(m map[string]any, box *cryptox.Box, outbound bool) {
	if m == nil {
		return
	}
	if outbound {
		if blob, _ := m["inline_identity_encrypted_blob"].(string); blob != "" {
			if plain, err := box.Decrypt(blob); err == nil {
				m["inline_secret"] = json.RawMessage(plain)
				delete(m, "inline_identity_encrypted_blob")
			}
		}
		if p, ok := m["inline_identity_encrypted_key_passphrase"].(*string); ok && p != nil {
			if plain, err := box.Decrypt(*p); err == nil {
				m["inline_key_passphrase"] = plain
				delete(m, "inline_identity_encrypted_key_passphrase")
			}
		}
		if p, ok := m["inline_identity_encrypted_key_passphrase"].(string); ok && p != "" {
			if plain, err := box.Decrypt(p); err == nil {
				m["inline_key_passphrase"] = plain
				delete(m, "inline_identity_encrypted_key_passphrase")
			}
		}
		return
	}
	if sec, ok := m["inline_secret"]; ok {
		raw, _ := json.Marshal(sec)
		if blob, err := box.Encrypt(string(raw)); err == nil {
			m["inline_identity_encrypted_blob"] = blob
			delete(m, "inline_secret")
		}
	}
	if kp, _ := m["inline_key_passphrase"].(string); kp != "" {
		if enc, err := box.Encrypt(kp); err == nil {
			m["inline_identity_encrypted_key_passphrase"] = enc
			delete(m, "inline_key_passphrase")
		}
	}
}

func (s *Server) rewriteOpSecrets(op model.ChangeOp, outbound bool) model.ChangeOp {
	if op.Entity != "identity" && op.Entity != "host" {
		return op
	}
	box := s.d.Box()
	if box == nil {
		return op
	}
	var m map[string]any
	if json.Unmarshal(op.Payload, &m) != nil {
		return op
	}
	if op.Entity == "identity" {
		s.rewriteIdentMap(m, box, outbound)
	}
	if op.Entity == "host" {
		s.rewriteHostMap(m, box, outbound)
	}
	b, _ := json.Marshal(m)
	op.Payload = b
	return op
}

func strconvAtoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
