package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
)

type HostRecord struct {
	Host             model.Host
	InlineAuthType   *string
	InlineBlob       *string
	InlinePassphrase *string
}

func (s *Store) ListHosts() ([]model.Host, error) {
	return s.queryHosts("")
}

func hostSelect() string {
	return `
SELECT h.id, h.folder_id, h.label, h.hostname, h.port, h.protocol, h.identity_id, h.jump_host_id,
       h.inline_identity_auth_type, h.last_connected_at, h.created_at, h.updated_at,
       i.label, i.auth_type, f.label, j.label,
       (SELECT GROUP_CONCAT(t.name, ',') FROM host_tags ht JOIN tags t ON t.id = ht.tag_id WHERE ht.host_id = h.id)
FROM hosts h
LEFT JOIN identities i ON i.id = h.identity_id AND i.deleted_at IS NULL
LEFT JOIN folders f ON f.id = h.folder_id AND f.deleted_at IS NULL
LEFT JOIN hosts j ON j.id = h.jump_host_id AND j.deleted_at IS NULL
WHERE h.deleted_at IS NULL`
}

func scanHost(rows interface {
	Scan(dest ...any) error
}) (model.Host, error) {
	var h model.Host
	var folderID, identID, jumpID, inlineAuth, lastConn, identLabel, identAuth, folderLabel, jumpLabel, tagList sql.NullString
	err := rows.Scan(
		&h.ID, &folderID, &h.Label, &h.Hostname, &h.Port, &h.Protocol, &identID, &jumpID,
		&inlineAuth, &lastConn, &h.CreatedAt, &h.UpdatedAt,
		&identLabel, &identAuth, &folderLabel, &jumpLabel, &tagList,
	)
	if err != nil {
		return h, err
	}
	h.FolderID = scanNullStr(folderID)
	h.IdentityID = scanNullStr(identID)
	h.JumpHostID = scanNullStr(jumpID)
	h.LastConnectedAt = scanNullStr(lastConn)
	h.FolderLabel = scanNullStr(folderLabel)
	h.JumpHostLabel = scanNullStr(jumpLabel)
	if identLabel.Valid {
		h.IdentityLabel = identLabel.String
	} else if inlineAuth.Valid {
		h.IdentityLabel = "one-time"
		h.HasInlineIdentity = true
	}
	if identAuth.Valid {
		h.IdentityAuthType = identAuth.String
	} else if inlineAuth.Valid {
		h.IdentityAuthType = inlineAuth.String
		h.HasInlineIdentity = true
	}
	if tagList.Valid && tagList.String != "" {
		h.Tags = strings.Split(tagList.String, ",")
	} else {
		h.Tags = []string{}
	}
	return h, nil
}

func (s *Store) queryHosts(where string, args ...any) ([]model.Host, error) {
	q := hostSelect() + " " + where + " ORDER BY h.label"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if out == nil {
		out = []model.Host{}
	}
	return out, rows.Err()
}

func (s *Store) GetHost(id string) (model.Host, error) {
	hosts, err := s.queryHosts("AND h.id = ?", id)
	if err != nil {
		return model.Host{}, err
	}
	if len(hosts) == 0 {
		return model.Host{}, ErrNotFound
	}
	return hosts[0], nil
}

func (s *Store) GetHostRecord(id string) (HostRecord, error) {
	var rec HostRecord
	var folderID, identID, jumpID, inlineAuth, inlineBlob, inlinePass, lastConn sql.NullString
	err := s.db.QueryRow(
		`SELECT id, folder_id, label, hostname, port, protocol, identity_id, jump_host_id,
		        inline_identity_auth_type, inline_identity_encrypted_blob, inline_identity_encrypted_key_passphrase,
		        last_connected_at, created_at, updated_at
		 FROM hosts WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(
		&rec.Host.ID, &folderID, &rec.Host.Label, &rec.Host.Hostname, &rec.Host.Port, &rec.Host.Protocol,
		&identID, &jumpID, &inlineAuth, &inlineBlob, &inlinePass, &lastConn, &rec.Host.CreatedAt, &rec.Host.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return rec, ErrNotFound
	}
	if err != nil {
		return rec, err
	}
	rec.Host.FolderID = scanNullStr(folderID)
	rec.Host.IdentityID = scanNullStr(identID)
	rec.Host.JumpHostID = scanNullStr(jumpID)
	rec.Host.LastConnectedAt = scanNullStr(lastConn)
	rec.InlineAuthType = scanNullStr(inlineAuth)
	rec.InlineBlob = scanNullStr(inlineBlob)
	rec.InlinePassphrase = scanNullStr(inlinePass)
	return rec, nil
}

type HostWrite struct {
	ID               string
	FolderID         *string
	Label            string
	Hostname         string
	Port             int
	Protocol         model.Protocol
	IdentityID       *string
	JumpHostID       *string
	InlineAuthType   *string
	InlineBlob       *string
	InlinePassphrase *string
	Tags             []string
}

func (s *Store) CreateHost(w HostWrite) (string, error) {
	id := w.ID
	if id == "" {
		id = idgen.New()
	}
	if w.Protocol == "" {
		w.Protocol = model.ProtocolSSH
	}
	if w.Port <= 0 {
		w.Port = 22
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO hosts(id, folder_id, label, hostname, port, protocol, identity_id, jump_host_id,
		                   inline_identity_auth_type, inline_identity_encrypted_blob, inline_identity_encrypted_key_passphrase,
		                   created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, nullStr(w.FolderID), w.Label, w.Hostname, w.Port, string(w.Protocol), nullStr(w.IdentityID), nullStr(w.JumpHostID),
		nullStr(w.InlineAuthType), nullStr(w.InlineBlob), nullStr(w.InlinePassphrase), ts, ts,
	)
	if err != nil {
		return "", err
	}
	if err := s.setHostTagsTx(tx, id, w.Tags, ts); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(s.hostPayload(id, w, ts, ts))
	if err := s.appendLog(tx, "host", id, "upsert", ts, json.RawMessage(payload)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) hostPayload(id string, w HostWrite, created, updated string) map[string]any {
	return map[string]any{
		"id": id, "folder_id": w.FolderID, "label": w.Label, "hostname": w.Hostname, "port": w.Port,
		"protocol": w.Protocol, "identity_id": w.IdentityID, "jump_host_id": w.JumpHostID,
		"inline_identity_auth_type": w.InlineAuthType, "inline_identity_encrypted_blob": w.InlineBlob,
		"inline_identity_encrypted_key_passphrase": w.InlinePassphrase, "tags": w.Tags,
		"created_at": created, "updated_at": updated,
	}
}

func (s *Store) UpdateHost(id string, w HostWrite) error {
	cur, err := s.GetHostRecord(id)
	if err != nil {
		return err
	}
	if w.Label == "" {
		w.Label = cur.Host.Label
	}
	if w.Hostname == "" {
		w.Hostname = cur.Host.Hostname
	}
	if w.Port <= 0 {
		w.Port = cur.Host.Port
	}
	if w.Protocol == "" {
		w.Protocol = cur.Host.Protocol
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`UPDATE hosts SET folder_id=?, label=?, hostname=?, port=?, protocol=?, identity_id=?, jump_host_id=?,
		        inline_identity_auth_type=?, inline_identity_encrypted_blob=?, inline_identity_encrypted_key_passphrase=?,
		        updated_at=? WHERE id=?`,
		nullStr(w.FolderID), w.Label, w.Hostname, w.Port, string(w.Protocol), nullStr(w.IdentityID), nullStr(w.JumpHostID),
		nullStr(w.InlineAuthType), nullStr(w.InlineBlob), nullStr(w.InlinePassphrase), ts, id,
	)
	if err != nil {
		return err
	}
	if w.Tags != nil {
		if err := s.setHostTagsTx(tx, id, w.Tags, ts); err != nil {
			return err
		}
	} else {
		w.Tags, _ = s.hostTagNames(id)
	}
	payload, _ := json.Marshal(s.hostPayload(id, w, cur.Host.CreatedAt, ts))
	if err := s.appendLog(tx, "host", id, "upsert", ts, json.RawMessage(payload)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteHost(id string) error {
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE hosts SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrNotFound
	}
	if err := s.appendLog(tx, "host", id, "delete", ts, map[string]any{"id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TouchHostConnected(id string) error {
	ts := now()
	_, err := s.db.Exec(`UPDATE hosts SET last_connected_at = ? WHERE id = ?`, ts, id)
	return err
}

func (s *Store) hostTagNames(hostID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT t.name FROM host_tags ht JOIN tags t ON t.id = ht.tag_id WHERE ht.host_id = ? ORDER BY t.name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if names == nil {
		names = []string{}
	}
	return names, rows.Err()
}

func (s *Store) setHostTagsTx(tx *sql.Tx, hostID string, tags []string, ts string) error {
	if _, err := tx.Exec(`DELETE FROM host_tags WHERE host_id = ?`, hostID); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, raw := range tags {
		name := NormalizeTag(raw)
		if !ValidTag(name) {
			return fmt.Errorf("invalid tag %q", raw)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		var tagID string
		err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID = idgen.New()
			if _, err := tx.Exec(
				`INSERT INTO tags(id, name, created_at, updated_at) VALUES(?,?,?,?)`,
				tagID, name, ts, ts,
			); err != nil {
				return err
			}
			if err := s.appendLog(tx, "tag", tagID, "upsert", ts, map[string]any{
				"id": tagID, "name": name, "created_at": ts, "updated_at": ts,
			}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO host_tags(host_id, tag_id) VALUES(?,?)`, hostID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListTagRecords() ([]model.Tag, error) {
	rows, err := s.db.Query(
		`SELECT id, name, created_at, updated_at FROM tags WHERE deleted_at IS NULL ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []model.Tag{}
	}
	return out, rows.Err()
}

func (s *Store) ListTags() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM tags WHERE deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

func (s *Store) Browse(folderID *string, q string) (model.BrowseResult, error) {
	q = strings.TrimSpace(q)
	res := model.BrowseResult{Folders: []model.Folder{}, Hosts: []model.Host{}, Breadcrumb: []model.Folder{}}
	if q != "" {
		res.SearchActive = true
		if strings.HasPrefix(strings.ToLower(q), "tag:") {
			tag := NormalizeTag(strings.TrimSpace(q[4:]))
			hosts, err := s.queryHosts(
				`AND h.id IN (SELECT ht.host_id FROM host_tags ht JOIN tags t ON t.id = ht.tag_id WHERE t.name = ?)`,
				tag,
			)
			if err != nil {
				return res, err
			}
			res.Hosts = hosts
			return res, nil
		}
		like := "%" + escapeLike(q) + "%"
		hosts, err := s.queryHosts(`AND (h.label LIKE ? ESCAPE '\' OR h.hostname LIKE ? ESCAPE '\')`, like, like)
		if err != nil {
			return res, err
		}
		res.Hosts = hosts
		return res, nil
	}

	bc, err := s.breadcrumb(folderID)
	if err != nil {
		return res, err
	}
	res.Breadcrumb = bc

	folderSQL := `SELECT id, parent_id, label, created_at, updated_at FROM folders WHERE deleted_at IS NULL AND `
	var folderArgs []any
	if folderID == nil {
		folderSQL += `parent_id IS NULL`
	} else {
		folderSQL += `parent_id = ?`
		folderArgs = append(folderArgs, *folderID)
	}
	folderSQL += ` ORDER BY label`
	rows, err := s.db.Query(folderSQL, folderArgs...)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var f model.Folder
		var parent sql.NullString
		if err := rows.Scan(&f.ID, &parent, &f.Label, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return res, err
		}
		f.ParentID = scanNullStr(parent)
		res.Folders = append(res.Folders, f)
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	var hosts []model.Host
	if folderID == nil {
		hosts, err = s.queryHosts(`AND h.folder_id IS NULL`)
	} else {
		hosts, err = s.queryHosts(`AND h.folder_id = ?`, *folderID)
	}
	if err != nil {
		return res, err
	}
	res.Hosts = hosts
	return res, nil
}

func (s *Store) breadcrumb(folderID *string) ([]model.Folder, error) {
	if folderID == nil {
		return []model.Folder{}, nil
	}
	var chain []model.Folder
	cur := folderID
	guard := map[string]struct{}{}
	for cur != nil {
		if _, ok := guard[*cur]; ok {
			break
		}
		guard[*cur] = struct{}{}
		f, err := s.GetFolder(*cur)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				break
			}
			return nil, err
		}
		chain = append([]model.Folder{f}, chain...)
		cur = f.ParentID
	}
	return chain, nil
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
