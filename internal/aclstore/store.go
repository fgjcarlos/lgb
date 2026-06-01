// Package aclstore implements a SQLite-backed store for role×tag write ACL rules.
// It mirrors the pattern of internal/plcstore: modernc.org/sqlite (pure Go,
// CGO_ENABLED=0 compatible), SetMaxOpenConns(1), migrate-on-open, sentinel errors.
//
// Requirements: TWA-STORE-1.1 through TWA-STORE-1.6.
package aclstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Sentinel errors returned by Store operations.
var (
	ErrRuleNotFound      = errors.New("acl rule not found")
	ErrRuleAlreadyExists = errors.New("acl rule already exists")
	ErrInvalidRole       = errors.New("invalid role: must be admin, operator, or viewer")
)

// validRoles is the allowlist for the role field.
var validRoles = map[string]bool{
	"admin":    true,
	"operator": true,
	"viewer":   true,
}

// ACLRule represents a single role×tag write-access rule.
// ID is the surrogate primary key assigned by the store; callers set it to 0
// on creation and use the value returned via ListRules/GetRule.
type ACLRule struct {
	ID         int64
	Role       string
	PLC        string
	Tag        string
	AllowWrite bool
}

// Store is a SQLite-backed ACL rule store.
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

// migrate creates the tag_acl table and lookup index if they do not exist, and
// enables SQLite foreign-key enforcement (OFF by default in modernc.org/sqlite).
func (s *Store) migrate(ctx context.Context) error {
	// PRAGMA foreign_keys = ON must be issued on the connection; with
	// SetMaxOpenConns(1) there is exactly one connection, so this is stable.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS tag_acl (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  role        TEXT    NOT NULL,
  plc         TEXT    NOT NULL,
  tag         TEXT    NOT NULL,
  allow_write INTEGER NOT NULL DEFAULT 0,
  UNIQUE(role, plc, tag)
);

CREATE INDEX IF NOT EXISTS idx_tag_acl_lookup ON tag_acl(role, plc, tag);
`)
	return err
}

// ─── Bootstrap ────────────────────────────────────────────────────────────────

// IsEmpty reports whether the tag_acl table contains zero rows.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tag_acl`).Scan(&count)
	return count == 0, err
}

// Seed bulk-inserts rules in a single transaction when the store IsEmpty.
// If the store is non-empty or rules is empty, Seed is a no-op (idempotent).
func (s *Store) Seed(ctx context.Context, rules []ACLRule) error {
	if len(rules) == 0 {
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

	for _, r := range rules {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tag_acl(role, plc, tag, allow_write) VALUES (?, ?, ?, ?)`,
			r.Role, r.PLC, r.Tag, boolToInt(r.AllowWrite)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// CreateRule inserts a new ACL rule. Returns ErrInvalidRole when the role is
// not in the allowlist (admin, operator, viewer). Returns ErrRuleAlreadyExists
// when a rule for the same (role, plc, tag) triple already exists.
func (s *Store) CreateRule(ctx context.Context, r ACLRule) error {
	if !validRoles[r.Role] {
		return ErrInvalidRole
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tag_acl(role, plc, tag, allow_write) VALUES (?, ?, ?, ?)`,
		r.Role, r.PLC, r.Tag, boolToInt(r.AllowWrite))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrRuleAlreadyExists
		}
		return err
	}
	return nil
}

// ListRules returns all ACL rules ordered by (role, plc, tag).
func (s *Store) ListRules(ctx context.Context) ([]ACLRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, plc, tag, allow_write FROM tag_acl ORDER BY role, plc, tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

// GetRule returns the ACL rule with the given surrogate id.
// Returns ErrRuleNotFound when no rule with that id exists.
func (s *Store) GetRule(ctx context.Context, id int64) (ACLRule, error) {
	var r ACLRule
	var allowWrite int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, role, plc, tag, allow_write FROM tag_acl WHERE id = ?`, id).
		Scan(&r.ID, &r.Role, &r.PLC, &r.Tag, &allowWrite)
	if errors.Is(err, sql.ErrNoRows) {
		return ACLRule{}, ErrRuleNotFound
	}
	if err != nil {
		return ACLRule{}, err
	}
	r.AllowWrite = allowWrite != 0
	return r, nil
}

// ListRulesByRole returns all ACL rules for the given role, ordered by (plc, tag).
func (s *Store) ListRulesByRole(ctx context.Context, role string) ([]ACLRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, plc, tag, allow_write FROM tag_acl WHERE role = ? ORDER BY plc, tag`,
		role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

// UpdateRule replaces all fields of the rule identified by id.
// Returns ErrRuleNotFound when no rule with that id exists.
// Returns ErrInvalidRole when the new role is not in the allowlist.
// Returns ErrRuleAlreadyExists when the new (role, plc, tag) collides with
// another existing row.
func (s *Store) UpdateRule(ctx context.Context, id int64, r ACLRule) error {
	if !validRoles[r.Role] {
		return ErrInvalidRole
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE tag_acl SET role = ?, plc = ?, tag = ?, allow_write = ? WHERE id = ?`,
		r.Role, r.PLC, r.Tag, boolToInt(r.AllowWrite), id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrRuleAlreadyExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// DeleteRule removes the ACL rule with the given id.
// Returns ErrRuleNotFound when no rule with that id exists.
func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tag_acl WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// CanWrite reports whether the given role is allowed to write the specified
// (plc, tag) pair. Returns (true, nil) only when an exact-match row with
// allow_write=1 exists. Any other case — no row, allow_write=0 — returns
// (false, nil). An error is returned only on store failure.
func (s *Store) CanWrite(ctx context.Context, role, plc, tag string) (bool, error) {
	var allowWrite int
	err := s.db.QueryRowContext(ctx,
		`SELECT allow_write FROM tag_acl WHERE role = ? AND plc = ? AND tag = ?`,
		role, plc, tag).Scan(&allowWrite)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return allowWrite != 0, nil
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// scanRules iterates over rows and scans each into an ACLRule.
func scanRules(rows *sql.Rows) ([]ACLRule, error) {
	var rules []ACLRule
	for rows.Next() {
		var r ACLRule
		var allowWrite int
		if err := rows.Scan(&r.ID, &r.Role, &r.PLC, &r.Tag, &allowWrite); err != nil {
			return nil, err
		}
		r.AllowWrite = allowWrite != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
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
