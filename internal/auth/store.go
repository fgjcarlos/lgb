package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserStore struct {
	db *sql.DB
}

func OpenUserStore(ctx context.Context, path string) (*UserStore, error) {
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
	s := &UserStore{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *UserStore) Close() error {
	return s.db.Close()
}

func (s *UserStore) Create(ctx context.Context, username, password string, role Role) (*User, error) {
	if !role.Valid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		username, string(hash), string(role), now.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, Role: role, CreatedAt: now}, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	return s.scanOne(ctx, `SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`, username)
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*User, error) {
	return s.scanOne(ctx, `SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?`, id)
}

func (s *UserStore) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, password_hash, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *UserStore) UpdateRole(ctx context.Context, id int64, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("invalid role %q", role)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(role), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *UserStore) UpdatePassword(ctx context.Context, id int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *UserStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *UserStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *UserStore) CountByRole(ctx context.Context, role Role) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, string(role)).Scan(&count)
	return count, err
}

func (s *UserStore) Authenticate(ctx context.Context, username, password string) (*User, error) {
	user, err := s.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserStore) scanOne(ctx context.Context, query string, args ...any) (*User, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	u, err := scanUserRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*User, error) {
	var u User
	var roleStr string
	var ts int64
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &roleStr, &ts); err != nil {
		return nil, err
	}
	u.Role = Role(roleStr)
	u.CreatedAt = time.Unix(ts, 0).UTC()
	return &u, nil
}

func scanUserRow(row *sql.Row) (*User, error) {
	var u User
	var roleStr string
	var ts int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &roleStr, &ts); err != nil {
		return nil, err
	}
	u.Role = Role(roleStr)
	u.CreatedAt = time.Unix(ts, 0).UTC()
	return &u, nil
}

func (s *UserStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'viewer',
		created_at INTEGER NOT NULL
	)`)
	return err
}

func isUniqueViolation(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) || containsStr(err.Error(), "UNIQUE constraint failed"))
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
