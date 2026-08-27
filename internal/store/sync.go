package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jdbnet/vantage/internal/model"
)

func (s *Store) ImportApply(op model.ChangeOp) error {
	localUpdated, err := s.localUpdatedAt(op.Entity, op.EntityID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if localUpdated != "" && !lwwWins(op.UpdatedAt, op.Origin, localUpdated, s.replicaID) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.applyOpTx(tx, op); err != nil {
		return err
	}
	if err := s.appendLog(tx, op.Entity, op.EntityID, op.Op, op.UpdatedAt, json.RawMessage(op.Payload)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApplyRemoteOp(op model.ChangeOp) error {
	if op.Origin == s.replicaID {
		return nil
	}
	localUpdated, err := s.localUpdatedAt(op.Entity, op.EntityID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if localUpdated != "" && !lwwWins(op.UpdatedAt, op.Origin, localUpdated, s.replicaID) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.applyOpTx(tx, op); err != nil {
		return err
	}
	if err := s.appendLogOrigin(tx, op.Entity, op.EntityID, op.Op, op.UpdatedAt, op.Origin, json.RawMessage(op.Payload)); err != nil {
		return err
	}
	return tx.Commit()
}

func lwwWins(remoteTs, remoteOrigin, localTs, localOrigin string) bool {
	if remoteTs > localTs {
		return true
	}
	if remoteTs < localTs {
		return false
	}
	return remoteOrigin > localOrigin
}

func (s *Store) localUpdatedAt(entity, id string) (string, error) {
	var table string
	switch entity {
	case "host":
		table = "hosts"
	case "folder":
		table = "folders"
	case "identity":
		table = "identities"
	case "snippet":
		table = "snippets"
	case "tag":
		table = "tags"
	case "known_host":
		table = "known_hosts"
	default:
		return "", nil
	}
	var updated sql.NullString
	err := s.db.QueryRow(`SELECT updated_at FROM `+table+` WHERE id = ?`, id).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !updated.Valid {
		return "", nil
	}
	return updated.String, nil
}

func (s *Store) applyOpTx(tx *sql.Tx, op model.ChangeOp) error {
	if op.Op == "delete" {
		return s.applyDeleteTx(tx, op)
	}
	var payload map[string]any
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		return err
	}
	switch op.Entity {
	case "folder":
		return upsertFolderTx(tx, payload, op.UpdatedAt)
	case "identity":
		return upsertIdentityTx(tx, payload, op.UpdatedAt)
	case "host":
		return upsertHostTx(tx, payload, op.UpdatedAt)
	case "snippet":
		return upsertSnippetTx(tx, payload, op.UpdatedAt)
	case "tag":
		return upsertTagTx(tx, payload, op.UpdatedAt)
	case "known_host":
		return upsertKnownHostTx(tx, payload, op.UpdatedAt)
	default:
		return fmt.Errorf("unknown entity %s", op.Entity)
	}
}

func (s *Store) applyDeleteTx(tx *sql.Tx, op model.ChangeOp) error {
	table := ""
	switch op.Entity {
	case "folder":
		table = "folders"
	case "identity":
		table = "identities"
	case "host":
		table = "hosts"
	case "snippet":
		table = "snippets"
	case "tag":
		table = "tags"
	case "known_host":
		_, err := tx.Exec(`DELETE FROM known_hosts WHERE id = ?`, op.EntityID)
		return err
	default:
		return nil
	}
	_, err := tx.Exec(`UPDATE `+table+` SET deleted_at = ?, updated_at = ? WHERE id = ?`, op.UpdatedAt, op.UpdatedAt, op.EntityID)
	return err
}

func strField(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func ptrField(m map[string]any, k string) *string {
	v, ok := m[k]
	if !ok || v == nil {
		return nil
	}
	s := fmt.Sprint(v)
	if s == "" || s == "<nil>" {
		return nil
	}
	return &s
}

func upsertFolderTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	_, err := tx.Exec(
		`INSERT INTO folders(id, parent_id, label, created_at, updated_at, deleted_at)
		 VALUES(?,?,?,?,?,NULL)
		 ON CONFLICT(id) DO UPDATE SET parent_id=excluded.parent_id, label=excluded.label, updated_at=excluded.updated_at, deleted_at=NULL`,
		id, nullStr(ptrField(p, "parent_id")), strField(p, "label"), coalesce(strField(p, "created_at"), ts), ts,
	)
	return err
}

func upsertIdentityTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	_, err := tx.Exec(
		`INSERT INTO identities(id, label, auth_type, encrypted_blob, encrypted_key_passphrase, created_at, updated_at, deleted_at)
		 VALUES(?,?,?,?,?,?,?,NULL)
		 ON CONFLICT(id) DO UPDATE SET label=excluded.label, auth_type=excluded.auth_type, encrypted_blob=excluded.encrypted_blob,
		   encrypted_key_passphrase=excluded.encrypted_key_passphrase, updated_at=excluded.updated_at, deleted_at=NULL`,
		id, strField(p, "label"), strField(p, "auth_type"), strField(p, "encrypted_blob"),
		nullStr(ptrField(p, "encrypted_key_passphrase")), coalesce(strField(p, "created_at"), ts), ts,
	)
	return err
}

func upsertSnippetTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	_, err := tx.Exec(
		`INSERT INTO snippets(id, label, command, created_at, updated_at, deleted_at)
		 VALUES(?,?,?,?,?,NULL)
		 ON CONFLICT(id) DO UPDATE SET label=excluded.label, command=excluded.command, updated_at=excluded.updated_at, deleted_at=NULL`,
		id, strField(p, "label"), strField(p, "command"), coalesce(strField(p, "created_at"), ts), ts,
	)
	return err
}

func upsertTagTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	name := strField(p, "name")
	_, err := tx.Exec(
		`INSERT INTO tags(id, name, created_at, updated_at, deleted_at)
		 VALUES(?,?,?,?,NULL)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at, deleted_at=NULL`,
		id, name, coalesce(strField(p, "created_at"), ts), ts,
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return nil
	}
	return err
}

func upsertHostTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	port := 22
	if v, ok := p["port"]; ok {
		switch n := v.(type) {
		case float64:
			port = int(n)
		case json.Number:
			i, _ := n.Int64()
			port = int(i)
		}
	}
	proto := strField(p, "protocol")
	if proto == "" {
		proto = "ssh"
	}
	_, err := tx.Exec(
		`INSERT INTO hosts(id, folder_id, label, hostname, port, protocol, identity_id, jump_host_id,
		                   inline_identity_auth_type, inline_identity_encrypted_blob, inline_identity_encrypted_key_passphrase,
		                   created_at, updated_at, deleted_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)
		 ON CONFLICT(id) DO UPDATE SET folder_id=excluded.folder_id, label=excluded.label, hostname=excluded.hostname,
		   port=excluded.port, protocol=excluded.protocol, identity_id=excluded.identity_id, jump_host_id=excluded.jump_host_id,
		   inline_identity_auth_type=excluded.inline_identity_auth_type, inline_identity_encrypted_blob=excluded.inline_identity_encrypted_blob,
		   inline_identity_encrypted_key_passphrase=excluded.inline_identity_encrypted_key_passphrase, updated_at=excluded.updated_at, deleted_at=NULL`,
		id, nullStr(ptrField(p, "folder_id")), strField(p, "label"), strField(p, "hostname"), port, proto,
		nullStr(ptrField(p, "identity_id")), nullStr(ptrField(p, "jump_host_id")),
		nullStr(ptrField(p, "inline_identity_auth_type")), nullStr(ptrField(p, "inline_identity_encrypted_blob")),
		nullStr(ptrField(p, "inline_identity_encrypted_key_passphrase")), coalesce(strField(p, "created_at"), ts), ts,
	)
	if err != nil {
		return err
	}
	if tags, ok := p["tags"].([]any); ok {
		names := make([]string, 0, len(tags))
		for _, t := range tags {
			names = append(names, fmt.Sprint(t))
		}
		if _, err := tx.Exec(`DELETE FROM host_tags WHERE host_id = ?`, id); err != nil {
			return err
		}
		for _, name := range names {
			name = NormalizeTag(name)
			if !ValidTag(name) {
				continue
			}
			var tagID string
			err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO host_tags(host_id, tag_id) VALUES(?,?)`, id, tagID); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertKnownHostTx(tx *sql.Tx, p map[string]any, ts string) error {
	id := strField(p, "id")
	port := 22
	if v, ok := p["port"]; ok {
		switch n := v.(type) {
		case float64:
			port = int(n)
		case json.Number:
			i, _ := n.Int64()
			port = int(i)
		}
	}
	hostname := strField(p, "hostname")
	_, err := tx.Exec(
		`INSERT INTO known_hosts(id, hostname, port, key_type, fingerprint, public_key, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET hostname=excluded.hostname, port=excluded.port, key_type=excluded.key_type,
		   fingerprint=excluded.fingerprint, public_key=excluded.public_key, updated_at=excluded.updated_at`,
		id, hostname, port, strField(p, "key_type"), strField(p, "fingerprint"),
		strField(p, "public_key"), coalesce(strField(p, "created_at"), ts), ts,
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		_, err = tx.Exec(
			`UPDATE known_hosts SET key_type=?, fingerprint=?, public_key=?, updated_at=? WHERE hostname=? AND port=?`,
			strField(p, "key_type"), strField(p, "fingerprint"), strField(p, "public_key"), ts, hostname, port,
		)
	}
	return err
}

func coalesce(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func (s *Store) Snapshot() (map[string]any, error) {
	folders, err := s.ListFolders()
	if err != nil {
		return nil, err
	}
	hosts, err := s.ListHosts()
	if err != nil {
		return nil, err
	}
	idents, err := s.ListIdentities()
	if err != nil {
		return nil, err
	}
	snips, err := s.ListSnippets()
	if err != nil {
		return nil, err
	}
	tags, err := s.ListTags()
	if err != nil {
		return nil, err
	}
	known, err := s.ListKnownHosts()
	if err != nil {
		return nil, err
	}
	identRecs := make([]map[string]any, 0, len(idents))
	for _, i := range idents {
		rec, err := s.GetIdentity(i.ID)
		if err != nil {
			continue
		}
		identRecs = append(identRecs, map[string]any{
			"id": i.ID, "label": i.Label, "auth_type": i.AuthType,
			"encrypted_blob": rec.Blob, "encrypted_key_passphrase": rec.Passphrase,
			"created_at": i.CreatedAt, "updated_at": i.UpdatedAt,
		})
	}
	hostRecs := make([]map[string]any, 0, len(hosts))
	for _, h := range hosts {
		rec, err := s.GetHostRecord(h.ID)
		if err != nil {
			continue
		}
		hostRecs = append(hostRecs, map[string]any{
			"id": h.ID, "folder_id": h.FolderID, "label": h.Label, "hostname": h.Hostname, "port": h.Port,
			"protocol": h.Protocol, "identity_id": h.IdentityID, "jump_host_id": h.JumpHostID,
			"inline_identity_auth_type": rec.InlineAuthType, "inline_identity_encrypted_blob": rec.InlineBlob,
			"inline_identity_encrypted_key_passphrase": rec.InlinePassphrase, "tags": h.Tags,
			"created_at": h.CreatedAt, "updated_at": h.UpdatedAt,
		})
	}
	return map[string]any{
		"replica_id":  s.replicaID,
		"folders":     folders,
		"hosts":       hostRecs,
		"identities":  identRecs,
		"snippets":    snips,
		"tags":        tags,
		"known_hosts": known,
	}, nil
}
