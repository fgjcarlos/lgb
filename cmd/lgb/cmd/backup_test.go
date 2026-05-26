package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

func TestBackupRun_NoRepos_ReturnsError(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Backup.Repos = nil

	d := &Deps{Config: cfg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runBackupTo(context.Background(), d, stdout, stderr)
	if err == nil {
		t.Fatal("expected error when no repos configured, got nil")
	}
	if !strings.Contains(err.Error(), "no repos configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBackupCheck_NoRepos_ReturnsError(t *testing.T) {
	cfg := testutil.MinimalConfig(t)
	cfg.Backup.Repos = nil

	d := &Deps{Config: cfg}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runBackupCheckTo(context.Background(), d, stdout, stderr)
	if err == nil {
		t.Fatal("expected error when no repos configured, got nil")
	}
	if !strings.Contains(err.Error(), "no repos configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBackupRun_NoConfig_ReturnsError(t *testing.T) {
	d := &Deps{Config: nil}
	err := runBackupTo(context.Background(), d, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when config is nil")
	}
}

func TestBackupCmd_Registered(t *testing.T) {
	root, _ := NewRoot()
	found := false
	for _, cmd := range root.Commands() {
		if cmd.Name() == "backup" {
			found = true
			var subNames []string
			for _, sub := range cmd.Commands() {
				subNames = append(subNames, sub.Name())
			}
			if !contains(subNames, "run") {
				t.Error("expected 'run' subcommand under backup")
			}
			if !contains(subNames, "check") {
				t.Error("expected 'check' subcommand under backup")
			}
		}
	}
	if !found {
		t.Error("expected 'backup' command to be registered on root")
	}
}

func TestConfigRepos_ConvertsCorrectly(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupSection{
			Repos: []config.BackupRepo{
				{URL: "/local/repo", Password: "secret1"},
				{URL: "s3:bucket/path", Password: "secret2"},
			},
		},
	}
	repos := configRepos(cfg)
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	if repos[0].URL != "/local/repo" || repos[1].URL != "s3:bucket/path" {
		t.Errorf("unexpected repo URLs: %+v", repos)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
