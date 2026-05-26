package auth

import (
	"testing"
	"time"
)

func TestTokenService_IssueAndValidate(t *testing.T) {
	svc := NewTokenService("test-secret-key-32bytes!!", 8*time.Hour)

	token, err := svc.Issue(1, "admin", RoleAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("UserID = %d; want 1", claims.UserID)
	}
	if claims.Username != "admin" {
		t.Errorf("Username = %q; want %q", claims.Username, "admin")
	}
	if claims.Role != RoleAdmin {
		t.Errorf("Role = %q; want %q", claims.Role, RoleAdmin)
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	svc := NewTokenService("test-secret-key-32bytes!!", -1*time.Hour)

	token, err := svc.Issue(1, "admin", RoleAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = svc.Validate(token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenService_InvalidToken(t *testing.T) {
	svc := NewTokenService("test-secret-key-32bytes!!", 8*time.Hour)

	_, err := svc.Validate("not-a-valid-token")
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestTokenService_WrongSecret(t *testing.T) {
	svc1 := NewTokenService("secret-one-32bytes!!!!!!", 8*time.Hour)
	svc2 := NewTokenService("secret-two-32bytes!!!!!!", 8*time.Hour)

	token, _ := svc1.Issue(1, "admin", RoleAdmin)
	_, err := svc2.Validate(token)
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid for wrong secret, got %v", err)
	}
}

func TestRole_Valid(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleAdmin, true},
		{RoleOperator, true},
		{RoleViewer, true},
		{Role("superuser"), false},
		{Role(""), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid() = %v; want %v", tt.role, got, tt.want)
		}
	}
}
