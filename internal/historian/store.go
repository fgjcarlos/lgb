package historian

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var ErrWriterBusy = errors.New("historian writer busy")

type Options struct {
	RetentionDays int
}

type Sample struct {
	PLCName   string
	Tag       string
	Timestamp time.Time
	Value     any
	Quality   string
}

type Query struct {
	PLCName string
	Tag     string
	From    time.Time
	To      time.Time
	Limit   int
}

type Store struct {
	db          *sql.DB
	options     Options
	writerBusy  atomic.Bool
	beforeWrite atomic.Pointer[func()]
}

func Open(ctx context.Context, path string, options Options) (*Store, error) {
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
	s := &Store{db: db, options: options}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SetBeforeWriteHook(fn func()) {
	if fn == nil {
		s.beforeWrite.Store(nil)
		return
	}
	s.beforeWrite.Store(&fn)
}

func (s *Store) WriterBusy() bool {
	return s.writerBusy.Load()
}

func (s *Store) InsertBatch(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	if !s.writerBusy.CompareAndSwap(false, true) {
		return ErrWriterBusy
	}
	defer s.writerBusy.Store(false)
	if hook := s.beforeWrite.Load(); hook != nil && *hook != nil {
		(*hook)()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO tag_samples(plc_name, tag_name, ts_unix_nano, value, quality) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, sample := range samples {
		quality := sample.Quality
		if quality == "" {
			quality = "good"
		}
		if _, err := stmt.ExecContext(ctx, sample.PLCName, sample.Tag, sample.Timestamp.UTC().UnixNano(), stringify(sample.Value), quality); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Query(ctx context.Context, q Query) ([]Sample, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT plc_name, tag_name, ts_unix_nano, value, quality FROM tag_samples WHERE plc_name = ? AND tag_name = ? AND ts_unix_nano >= ? AND ts_unix_nano <= ? ORDER BY ts_unix_nano ASC LIMIT ?`, q.PLCName, q.Tag, q.From.UTC().UnixNano(), q.To.UTC().UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var samples []Sample
	for rows.Next() {
		var sample Sample
		var ts int64
		var value string
		if err := rows.Scan(&sample.PLCName, &sample.Tag, &ts, &value, &sample.Quality); err != nil {
			return nil, err
		}
		sample.Timestamp = time.Unix(0, ts).UTC()
		sample.Value = value
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

func (s *Store) EnforceRetention(ctx context.Context, now time.Time) error {
	if s.options.RetentionDays <= 0 {
		return nil
	}
	cutoff := now.UTC().Add(-time.Duration(s.options.RetentionDays) * 24 * time.Hour).UnixNano()
	_, err := s.db.ExecContext(ctx, `DELETE FROM tag_samples WHERE ts_unix_nano < ?`, cutoff)
	return err
}

func (s *Store) VacuumInto(ctx context.Context, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "lgb-snapshot.db")
	_ = os.Remove(path)
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO %s", sqlQuote(path))); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS tag_samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plc_name TEXT NOT NULL,
		tag_name TEXT NOT NULL,
		ts_unix_nano INTEGER NOT NULL,
		value TEXT NOT NULL,
		quality TEXT NOT NULL DEFAULT 'good'
	);
	CREATE INDEX IF NOT EXISTS idx_tag_samples_lookup ON tag_samples(plc_name, tag_name, ts_unix_nano);`)
	return err
}

func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func sqlQuote(path string) string {
	out := "'"
	for _, r := range path {
		if r == '\'' {
			out += "''"
			continue
		}
		out += string(r)
	}
	return out + "'"
}
