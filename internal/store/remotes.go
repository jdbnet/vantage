package store

import (
	"database/sql"
	"errors"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
)

type RemoteRecord struct {
	Remote model.Remote
	Blob   string
}

type RemoteWrite struct {
	Label       string
	Protocol    string
	Host        string
	Port        int
	Bucket      string
	Region      string
	PathStyle   bool
	Secure      bool
	Username    string
	SftpKeyAuth bool
	Blob        string
	BlobSet     bool
}

func scanRemote(row interface{ Scan(...any) error }) (RemoteRecord, error) {
	var rec RemoteRecord
	var host, bucket, region, username, blob sql.NullString
	var pathStyle, secure, keyAuth int
	err := row.Scan(
		&rec.Remote.ID, &rec.Remote.Label, &rec.Remote.Protocol,
		&host, &rec.Remote.Port, &bucket, &region, &pathStyle, &secure,
		&username, &keyAuth, &blob, &rec.Remote.CreatedAt, &rec.Remote.UpdatedAt,
	)
	if err != nil {
		return rec, err
	}
	rec.Remote.Host = host.String
	rec.Remote.Bucket = bucket.String
	rec.Remote.Region = region.String
	rec.Remote.Username = username.String
	rec.Remote.PathStyle = pathStyle != 0
	rec.Remote.Secure = secure != 0
	rec.Remote.SftpKeyAuth = keyAuth != 0
	rec.Blob = blob.String
	rec.Remote.HasSecret = blob.Valid && blob.String != ""
	return rec, nil
}

const remoteCols = `id, label, protocol, host, port, bucket, region, path_style, secure, username, sftp_key_auth, encrypted_blob, created_at, updated_at`

func (s *Store) ListRemotes() ([]model.Remote, error) {
	rows, err := s.db.Query(`SELECT ` + remoteCols + ` FROM remotes WHERE deleted_at IS NULL ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Remote{}
	for rows.Next() {
		rec, err := scanRemote(rows)
		if err != nil {
			return nil, err
		}
		rec.Blob = ""
		out = append(out, rec.Remote)
	}
	return out, rows.Err()
}

func (s *Store) GetRemote(id string) (RemoteRecord, error) {
	row := s.db.QueryRow(`SELECT `+remoteCols+` FROM remotes WHERE id = ? AND deleted_at IS NULL`, id)
	rec, err := scanRemote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteRecord{}, ErrNotFound
	}
	return rec, err
}

func remotePayload(id string, w RemoteWrite, blob, created, updated string) map[string]any {
	return map[string]any{
		"id": id, "label": w.Label, "protocol": w.Protocol, "host": w.Host, "port": w.Port,
		"bucket": w.Bucket, "region": w.Region, "path_style": w.PathStyle, "secure": w.Secure,
		"username": w.Username, "sftp_key_auth": w.SftpKeyAuth, "encrypted_blob": blob,
		"created_at": created, "updated_at": updated,
	}
}

func (s *Store) CreateRemote(w RemoteWrite) (string, error) {
	id := idgen.New()
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO remotes(id, label, protocol, host, port, bucket, region, path_style, secure, username, sftp_key_auth, encrypted_blob, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, w.Label, w.Protocol, nullStr(strPtr(w.Host)), w.Port, nullStr(strPtr(w.Bucket)), nullStr(strPtr(w.Region)),
		boolInt(w.PathStyle), boolInt(w.Secure), nullStr(strPtr(w.Username)), boolInt(w.SftpKeyAuth), nullStr(strPtr(w.Blob)), ts, ts,
	)
	if err != nil {
		return "", err
	}
	if err := s.appendLog(tx, "remote", id, "upsert", ts, remotePayload(id, w, w.Blob, ts, ts)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateRemote(id string, w RemoteWrite) error {
	cur, err := s.GetRemote(id)
	if err != nil {
		return err
	}
	blob := cur.Blob
	if w.BlobSet {
		blob = w.Blob
	}
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`UPDATE remotes SET label=?, protocol=?, host=?, port=?, bucket=?, region=?, path_style=?, secure=?, username=?, sftp_key_auth=?, encrypted_blob=?, updated_at=? WHERE id=?`,
		w.Label, w.Protocol, nullStr(strPtr(w.Host)), w.Port, nullStr(strPtr(w.Bucket)), nullStr(strPtr(w.Region)),
		boolInt(w.PathStyle), boolInt(w.Secure), nullStr(strPtr(w.Username)), boolInt(w.SftpKeyAuth), nullStr(strPtr(blob)), ts, id,
	)
	if err != nil {
		return err
	}
	if err := s.appendLog(tx, "remote", id, "upsert", ts, remotePayload(id, w, blob, cur.Remote.CreatedAt, ts)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteRemote(id string) error {
	ts := now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE remotes SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, ts, ts, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := s.appendLog(tx, "remote", id, "delete", ts, map[string]any{"id": id}); err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
