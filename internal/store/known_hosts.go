package store

import (
	"database/sql"
	"errors"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
)

func (s *Store) GetKnownHost(hostname string, port int) (model.KnownHost, error) {
	var k model.KnownHost
	err := s.db.QueryRow(
		`SELECT id, hostname, port, key_type, fingerprint, public_key, created_at, updated_at
		 FROM known_hosts WHERE hostname = ? AND port = ?`,
		hostname, port,
	).Scan(&k.ID, &k.Hostname, &k.Port, &k.KeyType, &k.Fingerprint, &k.PublicKey, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

func (s *Store) ListKnownHosts() ([]model.KnownHost, error) {
	rows, err := s.db.Query(
		`SELECT id, hostname, port, key_type, fingerprint, public_key, created_at, updated_at
		 FROM known_hosts ORDER BY hostname, port`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.KnownHost
	for rows.Next() {
		var k model.KnownHost
		if err := rows.Scan(&k.ID, &k.Hostname, &k.Port, &k.KeyType, &k.Fingerprint, &k.PublicKey, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if out == nil {
		out = []model.KnownHost{}
	}
	return out, rows.Err()
}

func (s *Store) UpsertKnownHost(hostname string, port int, keyType, fingerprint, publicKey string) (model.KnownHost, error) {
	existing, err := s.GetKnownHost(hostname, port)
	ts := now()
	tx, errTx := s.db.Begin()
	if errTx != nil {
		return model.KnownHost{}, errTx
	}
	defer func() { _ = tx.Rollback() }()

	var k model.KnownHost
	if errors.Is(err, ErrNotFound) {
		k = model.KnownHost{
			ID:          idgen.New(),
			Hostname:    hostname,
			Port:        port,
			KeyType:     keyType,
			Fingerprint: fingerprint,
			PublicKey:   publicKey,
			CreatedAt:   ts,
			UpdatedAt:   ts,
		}
		_, err = tx.Exec(
			`INSERT INTO known_hosts(id, hostname, port, key_type, fingerprint, public_key, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			k.ID, k.Hostname, k.Port, k.KeyType, k.Fingerprint, k.PublicKey, k.CreatedAt, k.UpdatedAt,
		)
	} else if err != nil {
		return model.KnownHost{}, err
	} else {
		k = existing
		k.KeyType = keyType
		k.Fingerprint = fingerprint
		k.PublicKey = publicKey
		k.UpdatedAt = ts
		_, err = tx.Exec(
			`UPDATE known_hosts SET key_type=?, fingerprint=?, public_key=?, updated_at=? WHERE id=?`,
			k.KeyType, k.Fingerprint, k.PublicKey, k.UpdatedAt, k.ID,
		)
	}
	if err != nil {
		return model.KnownHost{}, err
	}
	if err := s.appendLog(tx, "known_host", k.ID, "upsert", ts, k); err != nil {
		return model.KnownHost{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.KnownHost{}, err
	}
	return k, nil
}
