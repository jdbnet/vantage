package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
)

type IdentityRecord struct {
	Identity   model.Identity
	Blob       string
	Passphrase *string
}

func (s *Store) ListIdentities() ([]model.Identity, error) {
	rows, err := s.db.Query(`SELECT id, label, auth_type, created_at, updated_at FROM identities WHERE deleted_at IS NULL ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Identity
	for rows.Next() {
		var i model.Identity
		if err := rows.Scan(&i.ID, &i.Label, &i.AuthType, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	if out == nil {
		out = []model.Identity{}
	}
	return out, rows.Err()
}

func (s *Store) GetIdentity(id string) (IdentityRecord, error) {
	var rec IdentityRecord
	var pass sql.NullString
	err := s.db.QueryRow(
		`SELECT id, label, auth_type, encrypted_blob, encrypted_key_passphrase, created_at, updated_at FROM identities WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(&rec.Identity.ID, &rec.Identity.Label, &rec.Identity.AuthType, &rec.Blob, &pass, &rec.Identity.CreatedAt, &rec.Identity.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return rec, ErrNotFound
	}
	rec.Passphrase = scanNullStr(pass)
	return rec, err
}

func (s *Store) CreateIdentity(label string, authType model.AuthType, blob string, pass *string) (string, error) {
	id := idgen.New()
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO identities(id, label, auth_type, encrypted_blob, encrypted_key_passphrase, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, label, string(authType), blob, nullStr(pass), ts, ts,
	)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"id": id, "label": label, "auth_type": authType,
		"encrypted_blob": blob, "encrypted_key_passphrase": pass,
		"created_at": ts, "updated_at": ts,
	}
	if err := s.appendLog(tx, "identity", id, "upsert", ts, payload); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateIdentity(id, label string, blob string, pass *string, passSet bool) error {
	rec, err := s.GetIdentity(id)
	if err != nil {
		return err
	}
	ts := now()
	if label == "" {
		label = rec.Identity.Label
	}
	if blob == "" {
		blob = rec.Blob
	}
	newPass := rec.Passphrase
	if passSet {
		newPass = pass
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`UPDATE identities SET label = ?, encrypted_blob = ?, encrypted_key_passphrase = ?, updated_at = ? WHERE id = ?`,
		label, blob, nullStr(newPass), ts, id,
	)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"id": id, "label": label, "auth_type": rec.Identity.AuthType,
		"encrypted_blob": blob, "encrypted_key_passphrase": newPass,
		"created_at": rec.Identity.CreatedAt, "updated_at": ts,
	}
	if err := s.appendLog(tx, "identity", id, "upsert", ts, payload); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteIdentity(id string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hosts WHERE identity_id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: identity in use by %d host(s)", ErrConflict, n)
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE identities SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrNotFound
	}
	if err := s.appendLog(tx, "identity", id, "delete", ts, map[string]any{"id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListFolders() ([]model.Folder, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, label, created_at, updated_at FROM folders WHERE deleted_at IS NULL ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Folder
	for rows.Next() {
		var f model.Folder
		var parent sql.NullString
		if err := rows.Scan(&f.ID, &parent, &f.Label, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.ParentID = scanNullStr(parent)
		out = append(out, f)
	}
	if out == nil {
		out = []model.Folder{}
	}
	return out, rows.Err()
}

func (s *Store) GetFolder(id string) (model.Folder, error) {
	var f model.Folder
	var parent sql.NullString
	err := s.db.QueryRow(
		`SELECT id, parent_id, label, created_at, updated_at FROM folders WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(&f.ID, &parent, &f.Label, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	f.ParentID = scanNullStr(parent)
	return f, err
}

func (s *Store) CreateFolder(label string, parentID *string) (string, error) {
	id := idgen.New()
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO folders(id, parent_id, label, created_at, updated_at) VALUES(?,?,?,?,?)`,
		id, nullStr(parentID), label, ts, ts,
	)
	if err != nil {
		return "", err
	}
	payload := map[string]any{"id": id, "parent_id": parentID, "label": label, "created_at": ts, "updated_at": ts}
	if err := s.appendLog(tx, "folder", id, "upsert", ts, payload); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateFolder(id, label string, parentID *string, parentSet bool) error {
	cur, err := s.GetFolder(id)
	if err != nil {
		return err
	}
	if label == "" {
		label = cur.Label
	}
	p := cur.ParentID
	if parentSet {
		p = parentID
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`UPDATE folders SET label = ?, parent_id = ?, updated_at = ? WHERE id = ?`, label, nullStr(p), ts, id)
	if err != nil {
		return err
	}
	payload := map[string]any{"id": id, "parent_id": p, "label": label, "created_at": cur.CreatedAt, "updated_at": ts}
	if err := s.appendLog(tx, "folder", id, "upsert", ts, payload); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteFolder(id string) error {
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE hosts SET folder_id = NULL, updated_at = ? WHERE folder_id = ? AND deleted_at IS NULL`, ts, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE folders SET parent_id = NULL, updated_at = ? WHERE parent_id = ? AND deleted_at IS NULL`, ts, id); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE folders SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrNotFound
	}
	if err := s.appendLog(tx, "folder", id, "delete", ts, map[string]any{"id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListSnippets() ([]model.Snippet, error) {
	rows, err := s.db.Query(`SELECT id, label, command, created_at, updated_at FROM snippets WHERE deleted_at IS NULL ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Snippet
	for rows.Next() {
		var sn model.Snippet
		if err := rows.Scan(&sn.ID, &sn.Label, &sn.Command, &sn.CreatedAt, &sn.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	if out == nil {
		out = []model.Snippet{}
	}
	return out, rows.Err()
}

func (s *Store) CreateSnippet(label, command string) (string, error) {
	id := idgen.New()
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`INSERT INTO snippets(id, label, command, created_at, updated_at) VALUES(?,?,?,?,?)`, id, label, command, ts, ts)
	if err != nil {
		return "", err
	}
	if err := s.appendLog(tx, "snippet", id, "upsert", ts, map[string]any{
		"id": id, "label": label, "command": command, "created_at": ts, "updated_at": ts,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateSnippet(id string, label, command *string) error {
	var cur model.Snippet
	err := s.db.QueryRow(`SELECT id, label, command, created_at, updated_at FROM snippets WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&cur.ID, &cur.Label, &cur.Command, &cur.CreatedAt, &cur.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if label != nil {
		cur.Label = *label
	}
	if command != nil {
		cur.Command = *command
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`UPDATE snippets SET label = ?, command = ?, updated_at = ? WHERE id = ?`, cur.Label, cur.Command, ts, id)
	if err != nil {
		return err
	}
	if err := s.appendLog(tx, "snippet", id, "upsert", ts, map[string]any{
		"id": id, "label": cur.Label, "command": cur.Command, "created_at": cur.CreatedAt, "updated_at": ts,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteSnippet(id string) error {
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE snippets SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrNotFound
	}
	if err := s.appendLog(tx, "snippet", id, "delete", ts, map[string]any{"id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

func NormalizeTag(raw string) string {
	name := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	return name
}

func ValidTag(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, c := range name {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if i == 0 && (c == '_' || c == '-') {
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}
