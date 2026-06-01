// Package aclstore_test tests the ACL SQLite store.
// Requirements: TWA-STORE-1.1 through TWA-STORE-1.6.
package aclstore_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fgjcarlos/lgb/internal/aclstore"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func openMemory(t *testing.T) *aclstore.Store {
	t.Helper()
	ctx := context.Background()
	s, err := aclstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("aclstore.Open(:memory:) error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleRule(role, plc, tag string, allowWrite bool) aclstore.ACLRule {
	return aclstore.ACLRule{
		Role:       role,
		PLC:        plc,
		Tag:        tag,
		AllowWrite: allowWrite,
	}
}

// ─── TWA-STORE-1.1: Open — creates tag_acl table ─────────────────────────────

// TestOpen_CreatesTables verifies that Open creates the tag_acl table by
// confirming that CRUD operations succeed (implying the table exists).
func TestOpen_CreatesTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "acl.db")
	s, err := aclstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer s.Close()

	// If table exists, CreateRule must succeed.
	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule after Open failed (table may not exist): %v", err)
	}
	rules, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules after CreateRule failed: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("ListRules len = %d; want 1", len(rules))
	}
}

// TestOpen_Idempotent verifies that opening the same DB twice does not fail
// and existing rows survive.
func TestOpen_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "acl_idempotent.db")

	s1, err := aclstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first Open error: %v", err)
	}
	if err := s1.CreateRule(ctx, sampleRule("admin", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	_ = s1.Close()

	s2, err := aclstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open error: %v", err)
	}
	defer s2.Close()

	rules, err := s2.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules after second Open: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("ListRules after second Open = %d; want 1 (rows must survive)", len(rules))
	}
}

// TestOpen_InMemorySeam verifies that ":memory:" returns a zero-row store.
func TestOpen_InMemorySeam(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	rules, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("ListRules on :memory: = %d; want 0", len(rules))
	}
}

// ─── TWA-STORE-1.2: IsEmpty / Seed ───────────────────────────────────────────

// TestIsEmpty_TrueOnEmpty verifies that a freshly opened store reports IsEmpty=true.
func TestIsEmpty_TrueOnEmpty(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatalf("IsEmpty error: %v", err)
	}
	if !empty {
		t.Error("IsEmpty = false on new store; want true")
	}
}

// TestSeed_PopulatesEmptyStore verifies that Seed inserts the given rules when
// the store is empty.
func TestSeed_PopulatesEmptyStore(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	rules := []aclstore.ACLRule{
		sampleRule("operator", "Silo-1", "Feed.Rate", true),
		sampleRule("admin", "Silo-1", "Temp.Set", true),
	}
	if err := s.Seed(ctx, rules); err != nil {
		t.Fatalf("Seed error: %v", err)
	}

	list, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListRules len = %d; want 2", len(list))
	}
}

// TestSeed_NoOpOnNonEmpty verifies that Seed is a no-op when the store already
// has rows.
func TestSeed_NoOpOnNonEmpty(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	// Seed one row first.
	if err := s.Seed(ctx, []aclstore.ACLRule{
		sampleRule("operator", "Silo-1", "Feed.Rate", true),
	}); err != nil {
		t.Fatalf("first Seed error: %v", err)
	}

	// Second Seed with different rules must be a no-op.
	if err := s.Seed(ctx, []aclstore.ACLRule{
		sampleRule("admin", "Silo-2", "Motor.Speed", true),
		sampleRule("viewer", "Silo-2", "Temp.Set", false),
	}); err != nil {
		t.Fatalf("second Seed error: %v", err)
	}

	list, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListRules len after second Seed = %d; want 1 (no-op)", len(list))
	}
}

// ─── TWA-STORE-1.3: CRUD — Create ────────────────────────────────────────────

// TestCreateRule_Valid verifies that a valid rule is inserted.
func TestCreateRule_Valid(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}

	rules, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("ListRules len = %d; want 1", len(rules))
	}
	if rules[0].Role != "operator" || rules[0].PLC != "Silo-1" || rules[0].Tag != "Feed.Rate" {
		t.Errorf("unexpected rule: %+v", rules[0])
	}
	if !rules[0].AllowWrite {
		t.Error("AllowWrite = false; want true")
	}
}

// TestCreateRule_Duplicate verifies that a duplicate (role, plc, tag) returns
// ErrRuleAlreadyExists.
func TestCreateRule_Duplicate(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	r := sampleRule("operator", "Silo-1", "Feed.Rate", true)
	if err := s.CreateRule(ctx, r); err != nil {
		t.Fatalf("first CreateRule error: %v", err)
	}
	err := s.CreateRule(ctx, r)
	if !errors.Is(err, aclstore.ErrRuleAlreadyExists) {
		t.Errorf("duplicate CreateRule = %v; want ErrRuleAlreadyExists", err)
	}
}

// TestCreateRule_InvalidRole verifies that an unknown role returns ErrInvalidRole.
func TestCreateRule_InvalidRole(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	err := s.CreateRule(ctx, sampleRule("superuser", "Silo-1", "Feed.Rate", true))
	if !errors.Is(err, aclstore.ErrInvalidRole) {
		t.Errorf("invalid role = %v; want ErrInvalidRole", err)
	}
}

// ─── TWA-STORE-1.4: CRUD — Read ──────────────────────────────────────────────

// TestListRules_ReturnsAll verifies that ListRules returns all rows ordered
// by (role, plc, tag).
func TestListRules_ReturnsAll(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	rules := []aclstore.ACLRule{
		sampleRule("viewer", "Silo-2", "Temp.Set", false),
		sampleRule("admin", "Silo-1", "Feed.Rate", true),
		sampleRule("operator", "Silo-1", "Motor.Speed", true),
	}
	for _, r := range rules {
		if err := s.CreateRule(ctx, r); err != nil {
			t.Fatalf("CreateRule error: %v", err)
		}
	}

	list, err := s.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListRules len = %d; want 3", len(list))
	}
	// Expect ORDER BY role, plc, tag: admin < operator < viewer
	if list[0].Role != "admin" {
		t.Errorf("list[0].Role = %q; want %q", list[0].Role, "admin")
	}
	if list[1].Role != "operator" {
		t.Errorf("list[1].Role = %q; want %q", list[1].Role, "operator")
	}
	if list[2].Role != "viewer" {
		t.Errorf("list[2].Role = %q; want %q", list[2].Role, "viewer")
	}
}

// TestGetRule_HappyPath verifies that GetRule returns the correct row.
func TestGetRule_HappyPath(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	rules, err := s.ListRules(ctx)
	if err != nil || len(rules) == 0 {
		t.Fatalf("ListRules error or empty: %v", err)
	}
	id := rules[0].ID

	got, err := s.GetRule(ctx, id)
	if err != nil {
		t.Fatalf("GetRule error: %v", err)
	}
	if got.Role != "operator" || got.PLC != "Silo-1" || got.Tag != "Feed.Rate" {
		t.Errorf("GetRule returned unexpected rule: %+v", got)
	}
}

// TestGetRule_NotFound verifies that GetRule returns ErrRuleNotFound for unknown id.
func TestGetRule_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	_, err := s.GetRule(ctx, 9999)
	if !errors.Is(err, aclstore.ErrRuleNotFound) {
		t.Errorf("GetRule unknown = %v; want ErrRuleNotFound", err)
	}
}

// TestListRulesByRole_Filters verifies that ListRulesByRole returns only rows
// matching the given role.
func TestListRulesByRole_Filters(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	if err := s.CreateRule(ctx, sampleRule("admin", "Silo-1", "Temp.Set", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	if err := s.CreateRule(ctx, sampleRule("viewer", "Silo-1", "Motor.Speed", false)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}

	list, err := s.ListRulesByRole(ctx, "operator")
	if err != nil {
		t.Fatalf("ListRulesByRole error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListRulesByRole(operator) len = %d; want 1", len(list))
	}
	if list[0].Role != "operator" {
		t.Errorf("unexpected role %q in result", list[0].Role)
	}
}

// ─── TWA-STORE-1.5: CRUD — Update / Delete ───────────────────────────────────

// TestUpdateRule_ReplacesFields verifies that UpdateRule replaces all fields.
func TestUpdateRule_ReplacesFields(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	rules, err := s.ListRules(ctx)
	if err != nil || len(rules) == 0 {
		t.Fatalf("ListRules error or empty: %v", err)
	}
	id := rules[0].ID

	updated := aclstore.ACLRule{Role: "admin", PLC: "Silo-2", Tag: "Temp.Set", AllowWrite: false}
	if err := s.UpdateRule(ctx, id, updated); err != nil {
		t.Fatalf("UpdateRule error: %v", err)
	}

	got, err := s.GetRule(ctx, id)
	if err != nil {
		t.Fatalf("GetRule error: %v", err)
	}
	if got.Role != "admin" || got.PLC != "Silo-2" || got.Tag != "Temp.Set" || got.AllowWrite {
		t.Errorf("UpdateRule result unexpected: %+v", got)
	}
}

// TestUpdateRule_NotFound verifies that UpdateRule returns ErrRuleNotFound for
// an unknown id.
func TestUpdateRule_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	err := s.UpdateRule(ctx, 9999, sampleRule("admin", "Silo-1", "Feed.Rate", true))
	if !errors.Is(err, aclstore.ErrRuleNotFound) {
		t.Errorf("UpdateRule unknown = %v; want ErrRuleNotFound", err)
	}
}

// TestDeleteRule_RemovesRow verifies that DeleteRule removes the row and a
// subsequent GetRule returns ErrRuleNotFound.
func TestDeleteRule_RemovesRow(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}
	rules, err := s.ListRules(ctx)
	if err != nil || len(rules) == 0 {
		t.Fatalf("ListRules error or empty: %v", err)
	}
	id := rules[0].ID

	if err := s.DeleteRule(ctx, id); err != nil {
		t.Fatalf("DeleteRule error: %v", err)
	}

	_, err = s.GetRule(ctx, id)
	if !errors.Is(err, aclstore.ErrRuleNotFound) {
		t.Errorf("GetRule after Delete = %v; want ErrRuleNotFound", err)
	}
}

// TestDeleteRule_NotFound verifies that DeleteRule returns ErrRuleNotFound for
// an unknown id.
func TestDeleteRule_NotFound(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	err := s.DeleteRule(ctx, 9999)
	if !errors.Is(err, aclstore.ErrRuleNotFound) {
		t.Errorf("DeleteRule unknown = %v; want ErrRuleNotFound", err)
	}
}

// ─── TWA-STORE-1.6: CanWrite lookup ──────────────────────────────────────────

// TestCanWrite_ExactMatchAllow verifies that CanWrite returns true for an exact
// match with allow_write=1.
func TestCanWrite_ExactMatchAllow(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", true)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}

	ok, err := s.CanWrite(ctx, "operator", "Silo-1", "Feed.Rate")
	if err != nil {
		t.Fatalf("CanWrite error: %v", err)
	}
	if !ok {
		t.Error("CanWrite = false; want true")
	}
}

// TestCanWrite_NoMatchingRow verifies that CanWrite returns false when no row
// matches (deny-by-default).
func TestCanWrite_NoMatchingRow(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	ok, err := s.CanWrite(ctx, "viewer", "Silo-1", "Feed.Rate")
	if err != nil {
		t.Fatalf("CanWrite error: %v", err)
	}
	if ok {
		t.Error("CanWrite = true; want false (no row)")
	}
}

// TestCanWrite_AllowWriteFalse verifies that CanWrite returns false when the
// row exists but allow_write=0.
func TestCanWrite_AllowWriteFalse(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	if err := s.CreateRule(ctx, sampleRule("operator", "Silo-1", "Feed.Rate", false)); err != nil {
		t.Fatalf("CreateRule error: %v", err)
	}

	ok, err := s.CanWrite(ctx, "operator", "Silo-1", "Feed.Rate")
	if err != nil {
		t.Fatalf("CanWrite error: %v", err)
	}
	if ok {
		t.Error("CanWrite = true; want false (allow_write=0)")
	}
}
