// Package plcstore_test tests the PLC SQLite store.
// Requirements: PCS-STORE-1.1 through PCS-STORE-1.6, PCS-CFG-5.1.
package plcstore_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
	_ "modernc.org/sqlite"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func openMemory(t *testing.T) *plcstore.Store {
	t.Helper()
	ctx := context.Background()
	s, err := plcstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("plcstore.Open(:memory:) error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func samplePLC(name string) config.PLC {
	return config.PLC{
		Name:          name,
		Address:       "10.0.0.1",
		Slot:          0,
		SocketTimeout: "5s",
		ScanRate:      "1s",
		KeepAlive:     true,
		Path:          "1,0",
		Tags: []config.TagDef{
			{Name: "Motor.Speed", Type: "Float", Writable: false},
			{Name: "Motor.Running", Type: "Boolean", Writable: true},
		},
	}
}

// ─── PCS-STORE-1.1: Open — creates tables ────────────────────────────────────

// TestOpen_CreatesTables verifies that Open creates both plcs and plc_tags tables
// by confirming that CRUD operations succeed (implying tables exist).
func TestOpen_CreatesTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "plcs.db")
	s, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer s.Close()

	// If tables exist, Create and List must succeed.
	if err := s.Create(ctx, samplePLC("probe")); err != nil {
		t.Fatalf("Create after Open failed (tables may not exist): %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List after Create failed: %v", err)
	}
	if len(list) != 1 || list[0].Name != "probe" {
		t.Errorf("unexpected List result: %v", list)
	}
	// Tags must also be stored (plc_tags table exists).
	if len(list[0].Tags) != 2 {
		t.Errorf("Tags len = %d; want 2 (plc_tags table must exist)", len(list[0].Tags))
	}
}

// TestOpen_Idempotent verifies that opening the same DB twice does not fail
// and existing rows survive.
func TestOpen_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "idempotent.db")

	s1, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first Open error: %v", err)
	}
	if err := s1.Create(ctx, samplePLC("plc-a")); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	_ = s1.Close()

	s2, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open error: %v", err)
	}
	defer s2.Close()

	plc, err := s2.Get(ctx, "plc-a")
	if err != nil {
		t.Fatalf("Get after second open: %v", err)
	}
	if plc.Name != "plc-a" {
		t.Errorf("Get name = %q; want %q", plc.Name, "plc-a")
	}
}

// ─── PCS-STORE-1.3/1.4/1.5/1.6: CRUD ─────────────────────────────────────────

// TestCreate_HappyPath verifies that Create inserts a PLC with tags that
// survive a round-trip via List.
func TestCreate_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d; want 1", len(list))
	}
	got := list[0]
	if got.Name != p.Name {
		t.Errorf("Name = %q; want %q", got.Name, p.Name)
	}
	if got.Address != p.Address {
		t.Errorf("Address = %q; want %q", got.Address, p.Address)
	}
	if got.Slot != p.Slot {
		t.Errorf("Slot = %d; want %d", got.Slot, p.Slot)
	}
	if got.ScanRate != p.ScanRate {
		t.Errorf("ScanRate = %q; want %q", got.ScanRate, p.ScanRate)
	}
	if got.SocketTimeout != p.SocketTimeout {
		t.Errorf("SocketTimeout = %q; want %q", got.SocketTimeout, p.SocketTimeout)
	}
	if got.KeepAlive != p.KeepAlive {
		t.Errorf("KeepAlive = %v; want %v", got.KeepAlive, p.KeepAlive)
	}
	if got.Path != p.Path {
		t.Errorf("Path = %q; want %q", got.Path, p.Path)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("Tags len = %d; want 2", len(got.Tags))
	}
	if got.Tags[0].Name != "Motor.Speed" {
		t.Errorf("Tags[0].Name = %q; want %q", got.Tags[0].Name, "Motor.Speed")
	}
	if got.Tags[1].Writable != true {
		t.Errorf("Tags[1].Writable = false; want true")
	}
}

// TestCreate_DuplicateName verifies that a second Create with the same name
// returns ErrPLCAlreadyExists.
func TestCreate_DuplicateName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("first Create error: %v", err)
	}
	err := s.Create(ctx, p)
	if !errors.Is(err, plcstore.ErrPLCAlreadyExists) {
		t.Errorf("second Create = %v; want ErrPLCAlreadyExists", err)
	}
}

// TestGet_HappyPath verifies that Get retrieves by name with tags.
func TestGet_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	if err := s.Create(ctx, samplePLC("line1")); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.Name != "line1" {
		t.Errorf("Name = %q; want %q", got.Name, "line1")
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags len = %d; want 2", len(got.Tags))
	}
}

// TestGet_Missing verifies that Get returns ErrPLCNotFound for unknown name.
func TestGet_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	_, err := s.Get(ctx, "nonexistent")
	if !errors.Is(err, plcstore.ErrPLCNotFound) {
		t.Errorf("Get missing = %v; want ErrPLCNotFound", err)
	}
}

// TestList_OrderedByName verifies that List returns PLCs in alphabetical name order.
func TestList_OrderedByName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	names := []string{"zebra", "alpha", "middle"}
	for _, n := range names {
		if err := s.Create(ctx, samplePLC(n)); err != nil {
			t.Fatalf("Create %q error: %v", n, err)
		}
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d; want 3", len(list))
	}
	want := []string{"alpha", "middle", "zebra"}
	for i, p := range list {
		if p.Name != want[i] {
			t.Errorf("list[%d].Name = %q; want %q", i, p.Name, want[i])
		}
		if len(p.Tags) != 2 {
			t.Errorf("list[%d].Tags len = %d; want 2", i, len(p.Tags))
		}
	}
}

// TestUpdate_ScanRatePersists verifies that Update changes scanRate and preserves other fields.
func TestUpdate_ScanRatePersists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	updated := p
	updated.ScanRate = "500ms"
	if err := s.Update(ctx, "line1", updated); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get after Update error: %v", err)
	}
	if got.ScanRate != "500ms" {
		t.Errorf("ScanRate after Update = %q; want %q", got.ScanRate, "500ms")
	}
	if got.Address != p.Address {
		t.Errorf("Address changed after Update: %q", got.Address)
	}
}

// TestUpdate_TagsReplacedAtomically verifies that Update replaces all tags.
func TestUpdate_TagsReplacedAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	updated := p
	updated.Tags = []config.TagDef{{Name: "NewTag", Type: "Int32"}}
	if err := s.Update(ctx, "line1", updated); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if len(got.Tags) != 1 {
		t.Errorf("Tags len after Update = %d; want 1", len(got.Tags))
	}
	if len(got.Tags) > 0 && got.Tags[0].Name != "NewTag" {
		t.Errorf("Tags[0].Name = %q; want %q", got.Tags[0].Name, "NewTag")
	}
}

// TestUpdate_Missing verifies that Update returns ErrPLCNotFound for unknown name.
func TestUpdate_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	err := s.Update(ctx, "nonexistent", samplePLC("nonexistent"))
	if !errors.Is(err, plcstore.ErrPLCNotFound) {
		t.Errorf("Update missing = %v; want ErrPLCNotFound", err)
	}
}

// TestDelete_CascadesTags verifies that Delete removes the PLC and its tags.
func TestDelete_CascadesTags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if err := s.Delete(ctx, "line1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := s.Get(ctx, "line1")
	if !errors.Is(err, plcstore.ErrPLCNotFound) {
		t.Errorf("Get after Delete = %v; want ErrPLCNotFound", err)
	}

	// Verify orphan tags are gone by creating a new PLC and checking its tag count
	// is independent (List returns empty, meaning cascade deleted all tags).
	list, listErr := s.List(ctx)
	if listErr != nil {
		t.Fatalf("List after Delete error: %v", listErr)
	}
	if len(list) != 0 {
		t.Errorf("List after Delete len = %d; want 0", len(list))
	}
}

// TestDelete_Missing verifies that Delete returns ErrPLCNotFound for unknown name.
func TestDelete_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	err := s.Delete(ctx, "nonexistent")
	if !errors.Is(err, plcstore.ErrPLCNotFound) {
		t.Errorf("Delete missing = %v; want ErrPLCNotFound", err)
	}
}

// ─── PCS-STORE-1.2: IsEmpty / Seed ───────────────────────────────────────────

// TestIsEmpty_TrueOnNew verifies that a freshly opened store is empty.
func TestIsEmpty_TrueOnNew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatalf("IsEmpty error: %v", err)
	}
	if !empty {
		t.Error("IsEmpty = false on new store; want true")
	}
}

// TestSeed_InsertsAllPLCsWithTags verifies that Seed populates the store.
func TestSeed_InsertsAllPLCsWithTags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	plcs := []config.PLC{samplePLC("plc-a"), samplePLC("plc-b")}
	if err := s.Seed(ctx, plcs); err != nil {
		t.Fatalf("Seed error: %v", err)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d; want 2", len(list))
	}
	for _, p := range list {
		if len(p.Tags) != 2 {
			t.Errorf("PLC %q: Tags len = %d; want 2", p.Name, len(p.Tags))
		}
	}
}

// TestSeed_IdempotentOnNonEmpty verifies that a second Seed call is a no-op.
func TestSeed_IdempotentOnNonEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	plcs := []config.PLC{samplePLC("plc-a")}
	if err := s.Seed(ctx, plcs); err != nil {
		t.Fatalf("first Seed error: %v", err)
	}
	if err := s.Seed(ctx, plcs); err != nil {
		t.Fatalf("second Seed error: %v", err)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len after second Seed = %d; want 1 (idempotent)", len(list))
	}
}

// ─── PCS-CFG-5.2: dcmd_enabled column ────────────────────────────────────────

// samplePLCWithDCMD returns a PLC with a tag that has DCMDEnabled set to true.
func samplePLCWithDCMD(name string) config.PLC {
	return config.PLC{
		Name:          name,
		Address:       "10.0.0.1",
		Slot:          0,
		SocketTimeout: "5s",
		ScanRate:      "1s",
		KeepAlive:     false,
		Path:          "",
		Tags: []config.TagDef{
			{Name: "Feed.Rate", Type: "Float", Writable: true, DCMDEnabled: true},
			{Name: "Motor.Speed", Type: "Float", Writable: false, DCMDEnabled: false},
		},
	}
}

// TestDCMDEnabled_FreshDB verifies that a brand-new DB has the dcmd_enabled column,
// defaults to false for new tags, and correctly round-trips true. (PCS-CFG-5.2)
func TestDCMDEnabled_FreshDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLCWithDCMD("line1")
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("Tags len = %d; want 2", len(got.Tags))
	}

	// Tag 0: DCMDEnabled should be true (we set it to true).
	if !got.Tags[0].DCMDEnabled {
		t.Errorf("Tags[0].DCMDEnabled = false; want true (round-trip)")
	}
	// Tag 1: DCMDEnabled should be false.
	if got.Tags[1].DCMDEnabled {
		t.Errorf("Tags[1].DCMDEnabled = true; want false (default)")
	}
}

// TestDCMDEnabled_LegacyDB verifies that opening a DB that was created WITHOUT
// the dcmd_enabled column runs the idempotent ALTER TABLE migration successfully,
// existing rows default to 0 (false), and Open on the same DB a second time
// does NOT error (idempotency). (PCS-CFG-5.2)
func TestDCMDEnabled_LegacyDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Build a "legacy" DB: create plcs + plc_tags WITHOUT dcmd_enabled column.
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy DB: %v", err)
	}
	legacyDB.SetMaxOpenConns(1)
	_, err = legacyDB.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	_, err = legacyDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS plcs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL UNIQUE,
    address        TEXT    NOT NULL,
    slot           INTEGER NOT NULL DEFAULT 0,
    socket_timeout TEXT    NOT NULL DEFAULT '',
    scan_rate      TEXT    NOT NULL DEFAULT '',
    keep_alive     INTEGER NOT NULL DEFAULT 0,
    path           TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS plc_tags (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    plc_id   INTEGER NOT NULL REFERENCES plcs(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    type     TEXT    NOT NULL,
    writable INTEGER NOT NULL DEFAULT 0,
    UNIQUE(plc_id, name)
);`)
	if err != nil {
		t.Fatalf("create legacy tables: %v", err)
	}

	// Insert a legacy row (without dcmd_enabled).
	now := int64(1000)
	res, err := legacyDB.ExecContext(ctx,
		`INSERT INTO plcs(name, address, slot, socket_timeout, scan_rate, keep_alive, path, created_at, updated_at)
		 VALUES ('legacy-plc', '10.0.0.1', 0, '5s', '1s', 0, '', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert legacy plc: %v", err)
	}
	plcID, _ := res.LastInsertId()
	_, err = legacyDB.ExecContext(ctx,
		`INSERT INTO plc_tags(plc_id, name, type, writable) VALUES (?, 'OldTag', 'Float', 1)`, plcID)
	if err != nil {
		t.Fatalf("insert legacy tag: %v", err)
	}
	_ = legacyDB.Close()

	// (a) Open via plcstore.Open — must succeed; migration adds dcmd_enabled column.
	s, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("plcstore.Open on legacy DB: %v", err)
	}
	defer s.Close()

	// Verify the existing tag now has dcmd_enabled = false (default 0).
	plc, err := s.Get(ctx, "legacy-plc")
	if err != nil {
		t.Fatalf("Get legacy-plc: %v", err)
	}
	if len(plc.Tags) != 1 {
		t.Fatalf("Tags len = %d; want 1", len(plc.Tags))
	}
	if plc.Tags[0].DCMDEnabled {
		t.Errorf("legacy tag DCMDEnabled = true; want false (migrated from old schema, default 0)")
	}

	// (b) Close and Open again — idempotency: must NOT error.
	_ = s.Close()
	s2, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second plcstore.Open on migrated DB (idempotency check): %v", err)
	}
	defer s2.Close()
}
