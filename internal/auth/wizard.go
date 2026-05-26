package auth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// EnsureAdminExists checks if any users exist. If not, it creates an admin
// user from the LGB_AUTH_ADMIN_USER and LGB_AUTH_ADMIN_PASSWORD env vars.
// Returns true if a user was created.
func EnsureAdminExists(ctx context.Context, store *UserStore, log *slog.Logger) (bool, error) {
	count, err := store.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("auth wizard: count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	username := os.Getenv("LGB_AUTH_ADMIN_USER")
	password := os.Getenv("LGB_AUTH_ADMIN_PASSWORD")
	if username == "" {
		username = "admin"
	}
	if password == "" {
		return false, fmt.Errorf("auth wizard: no users exist and LGB_AUTH_ADMIN_PASSWORD is not set — cannot create admin user")
	}

	user, err := store.Create(ctx, username, password, RoleAdmin)
	if err != nil {
		return false, fmt.Errorf("auth wizard: create admin: %w", err)
	}

	log.Info("auth wizard: created first admin user",
		slog.String("component", "auth"),
		slog.String("username", user.Username))
	return true, nil
}
