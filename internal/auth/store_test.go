package auth

import (
	"context"
	"testing"
)

func openTestStore(t *testing.T) *UserStore {
	t.Helper()
	s, err := OpenUserStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenUserStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUserStore_CreateAndGetByUsername(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user, err := s.Create(ctx, "alice", "password123", RoleAdmin)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if user.Username != "alice" {
		t.Errorf("Username = %q; want alice", user.Username)
	}
	if user.Role != RoleAdmin {
		t.Errorf("Role = %q; want admin", user.Role)
	}

	got, err := s.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("ID mismatch: %d vs %d", got.ID, user.ID)
	}
}

func TestUserStore_DuplicateUsername(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, _ = s.Create(ctx, "alice", "pass1", RoleAdmin)
	_, err := s.Create(ctx, "alice", "pass2", RoleViewer)
	if err != ErrUserAlreadyExists {
		t.Errorf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestUserStore_GetByID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user, _ := s.Create(ctx, "bob", "pass", RoleOperator)
	got, err := s.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Username != "bob" {
		t.Errorf("Username = %q; want bob", got.Username)
	}
}

func TestUserStore_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetByUsername(ctx, "nobody")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}

	_, err = s.GetByID(ctx, 999)
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserStore_List(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, _ = s.Create(ctx, "alice", "pass", RoleAdmin)
	_, _ = s.Create(ctx, "bob", "pass", RoleViewer)

	users, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestUserStore_UpdateRole(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user, _ := s.Create(ctx, "alice", "pass", RoleViewer)
	if err := s.UpdateRole(ctx, user.ID, RoleAdmin); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	got, _ := s.GetByID(ctx, user.ID)
	if got.Role != RoleAdmin {
		t.Errorf("Role = %q; want admin", got.Role)
	}
}

func TestUserStore_UpdatePassword(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user, _ := s.Create(ctx, "alice", "oldpass", RoleAdmin)
	if err := s.UpdatePassword(ctx, user.ID, "newpass"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, err := s.Authenticate(ctx, "alice", "newpass")
	if err != nil {
		t.Fatalf("Authenticate with new password: %v", err)
	}
	if got.ID != user.ID {
		t.Error("wrong user returned")
	}
}

func TestUserStore_Delete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user, _ := s.Create(ctx, "alice", "pass", RoleAdmin)
	if err := s.Delete(ctx, user.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.GetByID(ctx, user.ID)
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound after delete, got %v", err)
	}
}

func TestUserStore_DeleteNotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.Delete(context.Background(), 999)
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserStore_Authenticate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, _ = s.Create(ctx, "alice", "correctpass", RoleAdmin)

	user, err := s.Authenticate(ctx, "alice", "correctpass")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("Username = %q; want alice", user.Username)
	}

	_, err = s.Authenticate(ctx, "alice", "wrongpass")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound for wrong password, got %v", err)
	}
}

func TestUserStore_Count(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, _ := s.Count(ctx)
	if n != 0 {
		t.Errorf("expected 0 users, got %d", n)
	}

	_, _ = s.Create(ctx, "alice", "pass", RoleAdmin)
	n, _ = s.Count(ctx)
	if n != 1 {
		t.Errorf("expected 1 user, got %d", n)
	}
}

func TestUserStore_InvalidRole(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Create(context.Background(), "alice", "pass", Role("superuser"))
	if err == nil {
		t.Error("expected error for invalid role")
	}
}
