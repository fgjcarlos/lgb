// Package plcstore implements a SQLite-backed store for PLC configurations.
// It mirrors the pattern of internal/auth/store.go: modernc.org/sqlite (pure Go,
// CGO_ENABLED=0 compatible), SetMaxOpenConns(1), migrate-on-open, sentinel errors.
//
// Requirements: PCS-STORE-1.1 through PCS-STORE-1.6, PCS-CFG-5.1.
package plcstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"

	_ "modernc.org/sqlite"
)

// Sentinel errors returned by Store operations.
var (
	ErrPLCNotFound      = errors.New("plc not found")
	ErrPLCAlreadyExists = errors.New("plc already exists")
)

// Store is a SQLite-backed PLC configuration store.
// It is safe for concurrent reads; writes are serialised by SetMaxOpenConns(1).
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path, sets
// SetMaxOpenConns(1), enables foreign keys, and runs the schema migration.
// Use ":memory:" for in-process testing.
func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the plcs and plc_tags tables if they do not exist, and
// enables the SQLite foreign-key enforcement that is OFF by default in
// modernc.org/sqlite (required for ON DELETE CASCADE to fire).
func (s *Store) migrate(ctx context.Context) error {
	// PRAGMA foreign_keys = ON must be issued on the connection; with
	// SetMaxOpenConns(1) there is exactly one connection, so this is stable.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS plcs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL UNIQUE,
    address        TEXT    NOT NULL,
    slot           INTEGER NOT NULL DEFAULT 0,
    socket_timeout TEXT    NOT NULL DEFAULT '',
    scan_rate      TEXT    NOT NULL DEFAULT '',
    keep_alive     INTEGER NOT NULL DEFAULT 0,
    path           TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS plc_tags (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    plc_id    INTEGER NOT NULL REFERENCES plcs(id) ON DELETE CASCADE,
    name      TEXT    NOT NULL,
    type      TEXT    NOT NULL,
    writable  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(plc_id, name)
);`)
	return err
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// List returns all PLCs ordered by name, each with their tags populated.
func (s *Store) List(ctx context.Context) ([]config.PLC, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, address, slot, socket_timeout, scan_rate, keep_alive, path
		   FROM plcs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plcs []config.PLC
	var ids []int64
	for rows.Next() {
		var p config.PLC
		var id int64
		var keepAlive int
		if err := rows.Scan(&id, &p.Name, &p.Address, &p.Slot,
			&p.SocketTimeout, &p.ScanRate, &keepAlive, &p.Path); err != nil {
			return nil, err
		}
		p.KeepAlive = keepAlive != 0
		plcs = append(plcs, p)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Populate tags for each PLC.
	for i, id := range ids {
		tags, err := s.listTags(ctx, id)
		if err != nil {
			return nil, err
		}
		plcs[i].Tags = tags
	}
	return plcs, nil
}

// Get returns the PLC with the given name, including its tags.
// Returns ErrPLCNotFound when no PLC with that name exists.
func (s *Store) Get(ctx context.Context, name string) (config.PLC, error) {
	var p config.PLC
	var id int64
	var keepAlive int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, address, slot, socket_timeout, scan_rate, keep_alive, path
		   FROM plcs WHERE name = ?`, name).
		Scan(&id, &p.Name, &p.Address, &p.Slot,
			&p.SocketTimeout, &p.ScanRate, &keepAlive, &p.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return config.PLC{}, ErrPLCNotFound
	}
	if err != nil {
		return config.PLC{}, err
	}
	p.KeepAlive = keepAlive != 0

	tags, err := s.listTags(ctx, id)
	if err != nil {
		return config.PLC{}, err
	}
	p.Tags = tags
	return p, nil
}

// Create inserts a new PLC and its tags in a single transaction.
// Returns ErrPLCAlreadyExists when a PLC with the same name already exists.
func (s *Store) Create(ctx context.Context, p config.PLC) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO plcs(name, address, slot, socket_timeout, scan_rate, keep_alive, path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Address, p.Slot, p.SocketTimeout, p.ScanRate,
		boolToInt(p.KeepAlive), p.Path, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPLCAlreadyExists
		}
		return err
	}
	plcID, _ := res.LastInsertId()

	if err := insertTags(ctx, tx, plcID, p.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

// Update replaces the PLC record and its tags atomically.
// Returns ErrPLCNotFound when no PLC with that name exists.
func (s *Store) Update(ctx context.Context, name string, p config.PLC) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE plcs
		    SET name = ?, address = ?, slot = ?, socket_timeout = ?, scan_rate = ?,
		        keep_alive = ?, path = ?, updated_at = ?
		  WHERE name = ?`,
		p.Name, p.Address, p.Slot, p.SocketTimeout, p.ScanRate,
		boolToInt(p.KeepAlive), p.Path, now, name)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPLCAlreadyExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPLCNotFound
	}

	// Fetch the surrogate ID (name may have changed — use original name).
	var plcID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM plcs WHERE name = ?`, p.Name).
		Scan(&plcID); err != nil {
		return err
	}

	// Replace tags wholesale.
	if _, err := tx.ExecContext(ctx, `DELETE FROM plc_tags WHERE plc_id = ?`, plcID); err != nil {
		return err
	}
	if err := insertTags(ctx, tx, plcID, p.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a PLC and its tags in a single transaction.
// It explicitly deletes child rows (belt-and-suspenders) and relies on
// ON DELETE CASCADE as the primary mechanism.
// Returns ErrPLCNotFound when no PLC with that name exists.
func (s *Store) Delete(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Fetch the ID first so we can explicitly delete child rows.
	var plcID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM plcs WHERE name = ?`, name).Scan(&plcID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPLCNotFound
	}
	if err != nil {
		return err
	}

	// Explicit child delete (belt-and-suspenders alongside ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, `DELETE FROM plc_tags WHERE plc_id = ?`, plcID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plcs WHERE id = ?`, plcID); err != nil {
		return err
	}
	return tx.Commit()
}

// ─── Bootstrap ────────────────────────────────────────────────────────────────

// IsEmpty reports whether the plcs table contains zero rows.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plcs`).Scan(&count)
	return count == 0, err
}

// Seed bulk-inserts plcs in a single transaction when the store IsEmpty.
// If the store is non-empty or plcs is empty, Seed is a no-op (idempotent).
func (s *Store) Seed(ctx context.Context, plcs []config.PLC) error {
	if len(plcs) == 0 {
		return nil
	}
	empty, err := s.IsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Unix()
	for _, p := range plcs {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO plcs(name, address, slot, socket_timeout, scan_rate, keep_alive, path, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.Address, p.Slot, p.SocketTimeout, p.ScanRate,
			boolToInt(p.KeepAlive), p.Path, now, now)
		if err != nil {
			return err
		}
		plcID, _ := res.LastInsertId()
		if err := insertTags(ctx, tx, plcID, p.Tags); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// listTags fetches all tags for the given PLC surrogate ID, ordered by insertion.
func (s *Store) listTags(ctx context.Context, plcID int64) ([]config.TagDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, type, writable FROM plc_tags WHERE plc_id = ? ORDER BY id`, plcID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []config.TagDef
	for rows.Next() {
		var tag config.TagDef
		var writable int
		if err := rows.Scan(&tag.Name, &tag.Type, &writable); err != nil {
			return nil, err
		}
		tag.Writable = writable != 0
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// insertTags bulk-inserts tags for plcID inside tx.
func insertTags(ctx context.Context, tx *sql.Tx, plcID int64, tags []config.TagDef) error {
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plc_tags(plc_id, name, type, writable) VALUES (?, ?, ?, ?)`,
			plcID, tag.Name, tag.Type, boolToInt(tag.Writable)); err != nil {
			return err
		}
	}
	return nil
}

// boolToInt converts a Go bool to SQLite integer (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	return err != nil && containsStr(err.Error(), "UNIQUE constraint failed")
}

func containsStr(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
