package backup

import (
	"context"
	"errors"
	"fmt"
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

// BackupAll backs up every repository in order and stops at the first backup
// failure. After a repository's successful backup its retention policy, if any,
// is applied; forget failures do not stop later repositories and are returned
// joined once every repository was processed.
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
	return errors.Join(forgetErrs...)
}

// Snapshots returns the snapshots for the first configured repository.
// Returns an empty (non-nil) slice when no repositories are configured.
func (m *Manager) Snapshots(ctx context.Context) ([]Snapshot, error) {
	if len(m.repos) == 0 {
		return []Snapshot{}, nil
	}
	return m.runner.Snapshots(ctx, m.repos[0])
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
