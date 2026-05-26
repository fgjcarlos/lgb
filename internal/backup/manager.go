package backup

import (
	"context"
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

func (m *Manager) BackupAll(ctx context.Context, paths []string) error {
	for _, repo := range m.repos {
		if _, err := m.runner.Backup(ctx, repo, paths); err != nil {
			return fmt.Errorf("backup repo %s: %w", repo.URL, err)
		}
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
			_ = s.runOnce(ctx)
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
