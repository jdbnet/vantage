package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Store struct {
	db        *sql.DB
	replicaID string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." && filepath.Dir(path) != "" {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	s := &Store{db: db}
	id, err := s.ensureReplicaID()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.replicaID = id
	n, err := s.BackfillChangeLog()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if n > 0 {
		log.Printf("change log: backfilled %d missing rows", n)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ReplicaID() string {
	return s.replicaID
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func (s *Store) Meta(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) ensureReplicaID() (string, error) {
	v, ok, err := s.Meta("replica_id")
	if err != nil {
		return "", err
	}
	if ok && v != "" {
		return v, nil
	}
	id := idgen.New()
	if err := s.SetMeta("replica_id", id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) appendLog(tx *sql.Tx, entity, entityID, op, updatedAt string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO change_log(entity, entity_id, op, row_updated_at, origin_replica_id, payload) VALUES(?,?,?,?,?,?)`,
		entity, entityID, op, updatedAt, s.replicaID, string(raw),
	)
	return err
}

func (s *Store) appendLogOrigin(tx *sql.Tx, entity, entityID, op, updatedAt, origin string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO change_log(entity, entity_id, op, row_updated_at, origin_replica_id, payload) VALUES(?,?,?,?,?,?)`,
		entity, entityID, op, updatedAt, origin, string(raw),
	)
	return err
}

func nullStr(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}

func scanNullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newer(a, b string) bool {
	return strings.Compare(a, b) > 0
}

const DefaultChangePage = 500

func (s *Store) ChangesSince(seq int64) ([]model.ChangeOp, int64, error) {
	return s.queryChanges(seq, "", 0)
}

func (s *Store) ChangesSinceLimit(seq int64, limit int) ([]model.ChangeOp, int64, error) {
	return s.queryChanges(seq, "", limit)
}

func (s *Store) LocalChangesSince(seq int64, limit int) ([]model.ChangeOp, int64, error) {
	return s.queryChanges(seq, s.replicaID, limit)
}

func (s *Store) queryChanges(seq int64, origin string, limit int) ([]model.ChangeOp, int64, error) {
	q := `SELECT seq, entity, entity_id, op, row_updated_at, origin_replica_id, payload FROM change_log WHERE seq > ?`
	args := []any{seq}
	if origin != "" {
		q += ` AND origin_replica_id = ?`
		args = append(args, origin)
	}
	q += ` ORDER BY seq`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.ChangeOp
	var head int64 = seq
	for rows.Next() {
		var op model.ChangeOp
		var payload string
		if err := rows.Scan(&op.Seq, &op.Entity, &op.EntityID, &op.Op, &op.UpdatedAt, &op.Origin, &payload); err != nil {
			return nil, 0, err
		}
		op.Payload = json.RawMessage(payload)
		out = append(out, op)
		head = op.Seq
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 || (limit <= 0 || len(out) < limit) {
		max, err := s.HeadSeq()
		if err != nil {
			return nil, 0, err
		}
		if max > head {
			head = max
		}
	}
	return out, head, nil
}

func (s *Store) HeadSeq() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM change_log`).Scan(&n)
	return n, err
}
