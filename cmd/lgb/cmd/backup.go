package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/fgjcarlos/lgb/internal/backup"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/datadir"
	"github.com/fgjcarlos/lgb/internal/historian"
)

// NewBackupCmd returns the backup subcommand with run and check subcommands.
func NewBackupCmd(d *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage restic backups",
	}
	cmd.AddCommand(newBackupRunCmd(d))
	cmd.AddCommand(newBackupCheckCmd(d))
	return cmd
}

func newBackupRunCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run an immediate backup of the historian snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupTo(cmd.Context(), d, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func newBackupCheckCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Verify integrity of all configured backup repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupCheckTo(cmd.Context(), d, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func runBackupTo(ctx context.Context, d *Deps, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	cfg := d.Config
	if cfg == nil {
		return fmt.Errorf("backup: config not loaded")
	}
	if len(cfg.Backup.Repos) == 0 {
		fmt.Fprintln(stderr, "no backup repositories configured")
		return fmt.Errorf("backup: no repos configured")
	}

	resolvedPath, err := datadir.Resolve(cfg, d.DataDir)
	if err != nil {
		return fmt.Errorf("backup: resolve datadir: %w", err)
	}
	snapshotDir := filepath.Join(resolvedPath, "backup-tmp")

	if cfg.Historian.RetentionDays > 0 {
		dbPath := filepath.Join(resolvedPath, "lgb.db")
		store, err := historian.Open(ctx, dbPath, historian.Options{
			RetentionDays: cfg.Historian.RetentionDays,
		})
		if err != nil {
			return fmt.Errorf("backup: open historian: %w", err)
		}
		defer store.Close()

		fmt.Fprintln(stdout, "creating historian snapshot...")
		snapPath, err := store.VacuumInto(ctx, snapshotDir)
		if err != nil {
			return fmt.Errorf("backup: vacuum into: %w", err)
		}
		fmt.Fprintf(stdout, "snapshot created: %s\n", snapPath)
	}

	repos := configRepos(cfg)
	mgr := backup.NewManager(nil, repos)

	fmt.Fprintf(stdout, "backing up to %d repositories...\n", len(repos))
	start := time.Now()
	if err := mgr.BackupAll(ctx, []string{snapshotDir}); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	fmt.Fprintf(stdout, "backup complete in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func runBackupCheckTo(ctx context.Context, d *Deps, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	cfg := d.Config
	if cfg == nil {
		return fmt.Errorf("backup: config not loaded")
	}
	if len(cfg.Backup.Repos) == 0 {
		fmt.Fprintln(stderr, "no backup repositories configured")
		return fmt.Errorf("backup: no repos configured")
	}

	repos := configRepos(cfg)
	mgr := backup.NewManager(nil, repos)

	fmt.Fprintf(stdout, "checking %d repositories...\n", len(repos))
	if err := mgr.CheckAll(ctx); err != nil {
		return fmt.Errorf("backup check: %w", err)
	}
	fmt.Fprintln(stdout, "all repositories OK")
	return nil
}

func configRepos(cfg *config.Config) []backup.Repository {
	repos := make([]backup.Repository, len(cfg.Backup.Repos))
	for i, r := range cfg.Backup.Repos {
		repos[i] = backup.Repository{URL: r.URL, Password: r.Password}
	}
	return repos
}
