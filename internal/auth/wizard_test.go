package auth

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestEnsureAdminExists_CreatesFromEnv(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	t.Setenv("LGB_AUTH_ADMIN_USER", "superadmin")
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret-long-enough")

	created, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	if !created {
		t.Error("expected user to be created")
	}

	user, err := store.GetByUsername(ctx, "superadmin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if user.Role != RoleAdmin {
		t.Errorf("Role = %q; want admin", user.Role)
	}
}

func TestEnsureAdminExists_DefaultUsername(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	t.Setenv("LGB_AUTH_ADMIN_USER", "")
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret-long-enough")

	_, _ = EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	_, err := store.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("expected default 'admin' user, got: %v", err)
	}
}

func TestEnsureAdminExists_SkipsWhenUsersExist(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, _ = store.Create(ctx, "existing", "pass", RoleAdmin)
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret-long-enough")

	created, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	if created {
		t.Error("expected no user creation when users already exist")
	}
}

func TestEnsureAdminExists_ShortPasswordFails(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// 11 chars — one short of the 12-character minimum.
	t.Setenv("LGB_AUTH_ADMIN_USER", "admin")
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "tooshortpwd") // 11 chars

	_, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err == nil {
		t.Fatal("expected error for short admin password, got nil")
	}
}

func TestEnsureAdminExists_ErrorsWithoutPassword(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "")

	created, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err == nil {
		t.Fatal("expected error when no password env is set")
	}
	if created {
		t.Error("expected no user creation without a password")
	}
	want := `auth wizard: no users exist and LGB_AUTH_ADMIN_PASSWORD is not set — set it (at least 12 characters, at most 72 bytes; LGB_AUTH_ADMIN_USER defaults to "admin") to create the first admin user`
	if err.Error() != want {
		t.Errorf("error message:\n got: %s\nwant: %s", err, want)
	}
	if n, _ := store.Count(ctx); n != 0 {
		t.Errorf("expected 0 users after failed bootstrap, got %d", n)
	}
}
