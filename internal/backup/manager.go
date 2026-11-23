package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Manager struct {
	runner *Runner
	repos  []Repository
}

func NewManager(runner *Runner, repos []Repository) *Manager {
	if runner == nil {
		runner = NewRunner("")
	}
	return &Manager{runner: runner, repos: repos}
}

// RetentionError reports retention (forget) failures from a BackupAll run in
// which every backup succeeded, so fresh snapshots exist despite the error.
type RetentionError struct{ Err error }

func (e *RetentionError) Error() string { return e.Err.Error() }
func (e *RetentionError) Unwrap() error { return e.Err }

// BackupAll backs up every repository in order and stops at the first backup
// failure. After a repository's successful backup its retention policy, if any,
// is applied; forget failures do not stop later repositories. When every backup
// succeeded they are returned as a *RetentionError.
func (m *Manager) BackupAll(ctx context.Context, paths []string) error {
	var forgetErrs []error
	for _, repo := range m.repos {
		if _, err := m.runner.Backup(ctx, repo, paths); err != nil {
			return errors.Join(append(forgetErrs, fmt.Errorf("backup repo %s: %w", repo.URL, err))...)
		}
		if repo.Retention.Enabled() {
			if _, err := m.runner.Forget(ctx, repo, repo.Retention); err != nil {
				forgetErrs = append(forgetErrs, fmt.Errorf("forget repo %s: %w", repo.URL, err))
			}
		}
	}
	if len(forgetErrs) > 0 {
		return &RetentionError{Err: errors.Join(forgetErrs...)}
	}
	return nil
}

// Snapshots returns the snapshots for the first configured repository.
// Returns an empty (non-nil) slice when no repositories are configured.
func (m *Manager) Snapshots(ctx context.Context) ([]Snapshot, error) {
	if len(m.repos) == 0 {
		return []Snapshot{}, nil
	}
	return m.runner.Snapshots(ctx, m.repos[0])
}

var snapshotIDPattern = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// CheckRestore validates a restore request without side effects: repo is the
// 0-based index into the configured repositories, and dest must be an absolute
// path to a missing or empty directory, so a restore never overwrites live data.
func (m *Manager) CheckRestore(repo int, snapshotID, dest string) error {
	if repo < 0 || repo >= len(m.repos) {
		return fmt.Errorf("unknown backup repository 0-based index %d", repo)
	}
	if !snapshotIDPattern.MatchString(snapshotID) {
		return errors.New("snapshot_id must be 8-64 lowercase hex characters")
	}
	if !filepath.IsAbs(dest) {
		return errors.New("destination must be an absolute path")
	}
	dest = filepath.Clean(dest)
	info, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("restore destination %s is not a directory", dest)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("restore destination %s must be empty", dest)
	}
	return nil
}

// Restore validates the request with CheckRestore and restores snapshotID from
// the repo-th repository into dest.
func (m *Manager) Restore(ctx context.Context, repo int, snapshotID, dest string) error {
	if err := m.CheckRestore(repo, snapshotID, dest); err != nil {
		return err
	}
	if _, err := m.runner.Restore(ctx, m.repos[repo], snapshotID, filepath.Clean(dest)); err != nil {
		return fmt.Errorf("restore from repository %d: %w", repo, err)
	}
	return nil
}

func (m *Manager) CheckAll(ctx context.Context) error {
	for _, repo := range m.repos {
		if _, err := m.runner.Check(ctx, repo); err != nil {
			return fmt.Errorf("check repo %s: %w", repo.URL, err)
		}
	}
	return nil
}

type Scheduler struct {
	manager   *Manager
	paths     []string
	Every     time.Duration
	PreBackup func(ctx context.Context) error
	// OnResult, when set, receives the outcome of every scheduled run.
	OnResult func(err error)

	done chan struct{}
}

func NewScheduler(manager *Manager, paths []string, every time.Duration) *Scheduler {
	return &Scheduler{manager: manager, paths: paths, Every: every}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = s.run(ctx)
	}()
}

func (s *Scheduler) Stop() error {
	if s.done != nil {
		<-s.done
	}
	return nil
}

func (s *Scheduler) run(ctx context.Context) error {
	if s.Every <= 0 {
		return fmt.Errorf("backup schedule interval must be positive")
	}
	ticker := time.NewTicker(s.Every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			err := s.runOnce(ctx)
			// A run cut short by shutdown is not a backup failure.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if s.OnResult != nil {
				s.OnResult(err)
			}
		}
	}
}

// RunOnce executes a single backup cycle (PreBackup + BackupAll).
func (s *Scheduler) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx)
}

func (s *Scheduler) runOnce(ctx context.Context) error {
	if s.PreBackup != nil {
		if err := s.PreBackup(ctx); err != nil {
			return fmt.Errorf("pre-backup hook: %w", err)
		}
	}
	return s.manager.BackupAll(ctx, s.paths)
}
