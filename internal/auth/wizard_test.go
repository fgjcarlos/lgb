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
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret")

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
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret")

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
	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "s3cret")

	created, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}
	if created {
		t.Error("expected no user creation when users already exist")
	}
}

func TestEnsureAdminExists_ErrorsWithoutPassword(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	t.Setenv("LGB_AUTH_ADMIN_PASSWORD", "")

	_, err := EnsureAdminExists(ctx, store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err == nil {
		t.Error("expected error when no password env is set")
	}
}
