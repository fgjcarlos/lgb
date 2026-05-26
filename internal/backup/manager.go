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
	manager *Manager
	paths   []string
	Every   time.Duration
}

func NewScheduler(manager *Manager, paths []string, every time.Duration) *Scheduler {
	return &Scheduler{manager: manager, paths: paths, Every: every}
}

func (s *Scheduler) Run(ctx context.Context) error {
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
			if err := s.manager.BackupAll(ctx, s.paths); err != nil {
				return err
			}
		}
	}
}
