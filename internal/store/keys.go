package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
	"golang.org/x/crypto/argon2"
)

var DefaultScopes = []model.APIKeyScopeDef{
	{ID: "read:hosts", Label: "Read hosts", Description: "List and browse hosts, folders, identities, tags"},
	{ID: "write:hosts", Label: "Write hosts", Description: "Create, update, and delete inventory"},
	{ID: "read:audit", Label: "Read audit", Description: "View connection audit log"},
	{ID: "terminal:connect", Label: "Terminal", Description: "Open SSH, VNC, and RDP sessions"},
	{ID: "sftp:manage", Label: "SFTP", Description: "Browse and transfer files over SFTP"},
	{ID: "sync", Label: "Sync", Description: "Bidirectional inventory sync with a desktop client"},
}

func (s *Store) ListAPIKeys() ([]model.APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, label, key_prefix, scopes, expires_at, last_used_at, revoked_at, created_at FROM api_keys WHERE deleted_at IS NULL ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.APIKey
	now := time.Now().UTC()
	for rows.Next() {
		var k model.APIKey
		var scopes string
		var exp, last, rev sql.NullString
		if err := rows.Scan(&k.ID, &k.Label, &k.KeyPrefix, &scopes, &exp, &last, &rev, &k.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopes), &k.Scopes)
		k.ExpiresAt = scanNullStr(exp)
		k.LastUsedAt = scanNullStr(last)
		k.RevokedAt = scanNullStr(rev)
		if k.ExpiresAt != nil {
			if t, err := time.Parse(time.RFC3339Nano, *k.ExpiresAt); err == nil && t.Before(now) {
				k.Expired = true
			} else if t, err := time.Parse(time.RFC3339, *k.ExpiresAt); err == nil && t.Before(now) {
				k.Expired = true
			}
		}
		k.Active = k.RevokedAt == nil && !k.Expired
		out = append(out, k)
	}
	if out == nil {
		out = []model.APIKey{}
	}
	return out, rows.Err()
}

func (s *Store) CreateAPIKey(label string, scopes []string, expiresAt *string) (model.APIKey, string, error) {
	id := idgen.New()
	secret, prefix, hash, err := newAPIKeyMaterial()
	if err != nil {
		return model.APIKey{}, "", err
	}
	ts := now()
	rawScopes, _ := json.Marshal(scopes)
	tx, err := s.db.Begin()
	if err != nil {
		return model.APIKey{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO api_keys(id, label, key_prefix, key_hash, scopes, expires_at, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, label, prefix, hash, string(rawScopes), nullStr(expiresAt), ts, ts,
	)
	if err != nil {
		return model.APIKey{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return model.APIKey{}, "", err
	}
	k := model.APIKey{
		ID: id, Label: label, KeyPrefix: prefix, Scopes: scopes,
		ExpiresAt: expiresAt, CreatedAt: ts, Active: true,
	}
	return k, secret, nil
}

func (s *Store) DeleteAPIKey(id string) error {
	ts := now()
	res, err := s.db.Exec(`UPDATE api_keys SET deleted_at = ?, revoked_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, ts, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrNotFound
	}
	return nil
}

type AuthPrincipal struct {
	Kind   string
	Scopes []string
	KeyID  string
}

func (s *Store) LookupAPIKey(token string) (AuthPrincipal, error) {
	var empty AuthPrincipal
	// format vnt_k_<8hex>_<secret>
	parts := strings.SplitN(token, "_", 4)
	if len(parts) != 4 || parts[0] != "vnt" || parts[1] != "k" {
		return empty, ErrNotFound
	}
	keyPrefix := "vnt_k_" + parts[2]
	var id, hash, scopesJSON string
	var expires, revoked sql.NullString
	err := s.db.QueryRow(
		`SELECT id, key_hash, scopes, expires_at, revoked_at FROM api_keys WHERE key_prefix = ? AND deleted_at IS NULL`,
		keyPrefix,
	).Scan(&id, &hash, &scopesJSON, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	if revoked.Valid {
		return empty, ErrNotFound
	}
	if expires.Valid {
		if t, e := time.Parse(time.RFC3339Nano, expires.String); e == nil && t.Before(time.Now().UTC()) {
			return empty, ErrNotFound
		}
		if t, e := time.Parse(time.RFC3339, expires.String); e == nil && t.Before(time.Now().UTC()) {
			return empty, ErrNotFound
		}
	}
	if hashAPIKey(token) != hash {
		return empty, ErrNotFound
	}
	var scopes []string
	_ = json.Unmarshal([]byte(scopesJSON), &scopes)
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now(), id)
	return AuthPrincipal{Kind: "api_key", Scopes: scopes, KeyID: id}, nil
}

func HasScope(scopes []string, need string) bool {
	for _, s := range scopes {
		if s == need {
			return true
		}
	}
	return false
}

func newAPIKeyMaterial() (full, prefix, hash string, err error) {
	b := make([]byte, 18)
	if _, err = rand.Read(b); err != nil {
		return
	}
	idPart := hex.EncodeToString(b[:4])
	secret := hex.EncodeToString(b[4:])
	prefix = "vnt_k_" + idPart
	full = prefix + "_" + secret
	hash = hashAPIKey(full)
	return
}

func hashAPIKey(full string) string {
	sum := sha256.Sum256([]byte(full))
	// pepper with argon2 for stored hash
	salt := sum[:16]
	key := argon2.IDKey([]byte(full), salt, 1, 32*1024, 2, 32)
	return hex.EncodeToString(salt) + hex.EncodeToString(key)
}

func (s *Store) InsertAudit(host model.Host) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO connection_audit(host_id, host_label, hostname, port, protocol, jump_host_id, started_at)
		 VALUES(?,?,?,?,?,?,?)`,
		nullStr(strPtr(host.ID)), host.Label, host.Hostname, host.Port, string(host.Protocol), nullStr(host.JumpHostID), now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishAudit(id int64, started time.Time) error {
	dur := int(time.Since(started).Seconds())
	ended := now()
	_, err := s.db.Exec(
		`UPDATE connection_audit SET ended_at = ?, duration_seconds = ? WHERE id = ?`,
		ended, dur, id,
	)
	return err
}

func (s *Store) ListAudit(limit int, daysBack *int) ([]model.ConnectionAudit, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id, host_id, host_label, hostname, port, protocol, jump_host_id, started_at, ended_at, duration_seconds
	      FROM connection_audit`
	args := []any{}
	if daysBack != nil {
		cut := time.Now().UTC().AddDate(0, 0, -*daysBack).Format(time.RFC3339Nano)
		q += ` WHERE started_at >= ?`
		args = append(args, cut)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConnectionAudit
	for rows.Next() {
		var a model.ConnectionAudit
		var hostID, jumpID, ended sql.NullString
		var dur sql.NullInt64
		if err := rows.Scan(&a.ID, &hostID, &a.HostLabel, &a.Hostname, &a.Port, &a.Protocol, &jumpID, &a.StartedAt, &ended, &dur); err != nil {
			return nil, err
		}
		a.HostID = scanNullStr(hostID)
		a.JumpHostID = scanNullStr(jumpID)
		a.EndedAt = scanNullStr(ended)
		if dur.Valid {
			v := int(dur.Int64)
			a.DurationSeconds = &v
		}
		out = append(out, a)
	}
	if out == nil {
		out = []model.ConnectionAudit{}
	}
	return out, rows.Err()
}

func (s *Store) DefaultSettings() model.Settings {
	return model.Settings{
		ListenAddr:         ":7687",
		GuacdAddr:          "127.0.0.1:4822",
		TerminalTheme:      "default",
		TerminalFontFamily: "DM Mono, ui-monospace, monospace",
		TerminalFontSize:   14,
		DisplayColorDepth:  24,
		DisplayWidth:       1920,
		DisplayHeight:      1080,
		AccentColor:        "#1ebe8a",
		AuditLogEnabled:    true,
		ReplicaID:          s.replicaID,
	}
}

func NormalizeAccentColor(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return "", false
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return "", false
		}
	}
	return "#" + strings.ToLower(s), true
}

func (s *Store) LoadSettings() (model.Settings, error) {
	st := s.DefaultSettings()
	if v, ok, _ := s.Meta("listen_addr"); ok {
		st.ListenAddr = v
	}
	if v, ok, _ := s.Meta("guacd_addr"); ok {
		st.GuacdAddr = v
	}
	if v, ok, _ := s.Meta("shared_files_dir"); ok {
		st.SharedFilesDir = v
	}
	if v, ok, _ := s.Meta("terminal_theme"); ok {
		st.TerminalTheme = v
	}
	if v, ok, _ := s.Meta("terminal_font_family"); ok {
		st.TerminalFontFamily = v
	}
	if v, ok, _ := s.Meta("terminal_font_size"); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.TerminalFontSize = n
		}
	}
	if v, ok, _ := s.Meta("display_color_depth"); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.DisplayColorDepth = n
		}
	}
	if v, ok, _ := s.Meta("display_width"); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.DisplayWidth = n
		}
	}
	if v, ok, _ := s.Meta("display_height"); ok {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.DisplayHeight = n
		}
	}
	if v, ok, _ := s.Meta("accent_color"); ok {
		if n, valid := NormalizeAccentColor(v); valid {
			st.AccentColor = n
		}
	}
	if v, ok, _ := s.Meta("sync_url"); ok {
		st.SyncURL = v
	}
	if v, ok, _ := s.Meta("sync_api_key"); ok && v != "" {
		st.SyncAPIKeySet = true
	}
	if v, ok, _ := s.Meta("audit_log_enabled"); ok {
		st.AuditLogEnabled = v != "0" && v != "false"
	}
	st.ReplicaID = s.replicaID
	return st, nil
}

func (s *Store) SaveSettings(patch map[string]string) error {
	allowed := map[string]struct{}{
		"listen_addr": {}, "guacd_addr": {}, "shared_files_dir": {}, "terminal_theme": {},
		"terminal_font_family": {}, "terminal_font_size": {},
		"display_color_depth": {}, "display_width": {}, "display_height": {},
		"accent_color": {}, "sync_url": {}, "sync_api_key": {}, "audit_log_enabled": {},
	}
	for k, v := range patch {
		if _, ok := allowed[k]; !ok {
			continue
		}
		if k == "accent_color" {
			n, valid := NormalizeAccentColor(v)
			if !valid {
				return fmt.Errorf("invalid accent_color")
			}
			v = n
		}
		if err := s.SetMeta(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SyncAPIKey() (string, bool, error) {
	return s.Meta("sync_api_key")
}
