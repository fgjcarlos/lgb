package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Snapshot represents a single restic snapshot as returned by `restic snapshots --json`.
type Snapshot struct {
	ID       string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Paths    []string  `json:"paths"`
}

type Repository struct {
	URL       string
	Password  string
	Retention RetentionPolicy
}

// RetentionPolicy maps to `restic forget --keep-* [--prune]`. Zero values are omitted.
type RetentionPolicy struct {
	KeepHourly  int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	Prune       bool
}

// Enabled reports whether the policy keeps anything; without a keep-* rule
// forget has nothing to apply.
func (p RetentionPolicy) Enabled() bool {
	return p.KeepHourly > 0 || p.KeepDaily > 0 || p.KeepWeekly > 0 || p.KeepMonthly > 0
}

type Runner struct {
	bin string
}

type CommandResult struct {
	RawJSON json.RawMessage
}

func NewRunner(bin string) *Runner {
	if bin == "" {
		bin = "restic"
	}
	return &Runner{bin: bin}
}

func (r *Runner) Init(ctx context.Context, repo Repository) error {
	_, err := r.run(ctx, repo, "init")
	return err
}

func (r *Runner) Backup(ctx context.Context, repo Repository, paths []string) (CommandResult, error) {
	args := append([]string{"backup"}, paths...)
	return r.run(ctx, repo, args...)
}

// Forget applies the retention policy with `restic forget`, pruning unreferenced
// data when policy.Prune is set. It refuses a policy without keep-* rules.
func (r *Runner) Forget(ctx context.Context, repo Repository, policy RetentionPolicy) (CommandResult, error) {
	if !policy.Enabled() {
		return CommandResult{}, fmt.Errorf("restic forget: retention policy has no keep-* rule")
	}
	args := []string{"forget"}
	for _, k := range []struct {
		flag string
		n    int
	}{
		{"--keep-hourly", policy.KeepHourly},
		{"--keep-daily", policy.KeepDaily},
		{"--keep-weekly", policy.KeepWeekly},
		{"--keep-monthly", policy.KeepMonthly},
	} {
		if k.n > 0 {
			args = append(args, k.flag, strconv.Itoa(k.n))
		}
	}
	if policy.Prune {
		args = append(args, "--prune")
	}
	return r.run(ctx, repo, args...)
}

func (r *Runner) Check(ctx context.Context, repo Repository) (CommandResult, error) {
	return r.run(ctx, repo, "check")
}

func (r *Runner) Restore(ctx context.Context, repo Repository, snapshotID, dest string) (CommandResult, error) {
	if err := requireCleanDestination(dest); err != nil {
		return CommandResult{}, err
	}
	return r.run(ctx, repo, "restore", snapshotID, "--target", dest)
}

// Snapshots returns the list of snapshots in the given repository by shelling
// out to `restic snapshots --json`. It always returns a non-nil slice.
func (r *Runner) Snapshots(ctx context.Context, repo Repository) ([]Snapshot, error) {
	fullArgs := []string{"-r", repo.URL, "snapshots", "--json"}
	cmd := exec.CommandContext(ctx, r.bin, fullArgs...)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repo.Password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots failed: %w: %s", err, string(out))
	}
	var snaps []Snapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("parse snapshots JSON: %w", err)
	}
	if snaps == nil {
		snaps = []Snapshot{}
	}
	return snaps, nil
}

func (r *Runner) run(ctx context.Context, repo Repository, args ...string) (CommandResult, error) {
	fullArgs := append([]string{"-r", repo.URL}, args...)
	fullArgs = append(fullArgs, "--json")
	cmd := exec.CommandContext(ctx, r.bin, fullArgs...)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+repo.Password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CommandResult{RawJSON: out}, fmt.Errorf("restic %v failed: %w: %s", args, err, string(out))
	}
	return CommandResult{RawJSON: out}, nil
}

func requireCleanDestination(dest string) error {
	entries, err := os.ReadDir(dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(dest, 0o755)
		}
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("restore destination %s must be empty", filepath.Clean(dest))
	}
	return nil
}
