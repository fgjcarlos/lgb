package plcstore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

// TestRetentionDays_RoundTrip verifies #85: per-PLC and per-tag retentionDays
// survive Create, Get, Update, List and Seed; unset values stay 0 (inherit).
func TestRetentionDays_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	p.RetentionDays = 30
	p.Tags = []config.TagDef{
		{Name: "Speed", Type: "Float", RetentionDays: 7},
		{Name: "Running", Type: "Boolean"},
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetentionDays != 30 || got.Tags[0].RetentionDays != 7 || got.Tags[1].RetentionDays != 0 {
		t.Errorf("after Create: plc %d, tags %d/%d; want 30, 7/0", got.RetentionDays, got.Tags[0].RetentionDays, got.Tags[1].RetentionDays)
	}

	p.RetentionDays = 0
	p.Tags[1].RetentionDays = 365
	if err := s.Update(ctx, "line1", p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if l := list[0]; l.RetentionDays != 0 || l.Tags[0].RetentionDays != 7 || l.Tags[1].RetentionDays != 365 {
		t.Errorf("after Update: plc %d, tags %d/%d; want 0, 7/365", l.RetentionDays, l.Tags[0].RetentionDays, l.Tags[1].RetentionDays)
	}

	seeded := openMemory(t)
	seed := samplePLC("seeded")
	seed.RetentionDays = 14
	seed.Tags = []config.TagDef{{Name: "Speed", Type: "Float", RetentionDays: 2}}
	if err := seeded.Seed(ctx, []config.PLC{seed}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	sg, err := seeded.Get(ctx, "seeded")
	if err != nil {
		t.Fatalf("Get seeded: %v", err)
	}
	if sg.RetentionDays != 14 || sg.Tags[0].RetentionDays != 2 {
		t.Errorf("after Seed: plc %d, tag %d; want 14, 2", sg.RetentionDays, sg.Tags[0].RetentionDays)
	}
}

// TestRetentionDays_LegacyDB verifies #85: opening a store created before the
// retention columns existed keeps every PLC and tag, defaults both new
// columns to 0 (inherit), and reopening is idempotent.
func TestRetentionDays_LegacyDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy DB: %v", err)
	}
	legacyDB.SetMaxOpenConns(1)
	_, err = legacyDB.ExecContext(ctx, `
CREATE TABLE plcs (
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
CREATE TABLE plc_tags (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    plc_id              INTEGER NOT NULL REFERENCES plcs(id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    type                TEXT    NOT NULL,
    writable            INTEGER NOT NULL DEFAULT 0,
    dcmd_enabled        INTEGER NOT NULL DEFAULT 0,
    opcua_write_enabled INTEGER NOT NULL DEFAULT 0,
    deadband_abs        REAL    NOT NULL DEFAULT 0,
    deadband_pct        REAL    NOT NULL DEFAULT 0,
    UNIQUE(plc_id, name)
);
INSERT INTO plcs(name, address, slot, scan_rate, created_at, updated_at) VALUES ('legacy-plc', '10.0.0.1', 2, '500ms', 1, 1);
INSERT INTO plc_tags(plc_id, name, type, writable, dcmd_enabled, deadband_abs) VALUES (1, 'OldTag', 'Float', 1, 1, 0.5);`)
	if err != nil {
		t.Fatalf("create legacy DB: %v", err)
	}
	_ = legacyDB.Close()

	s, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("plcstore.Open on legacy DB: %v", err)
	}
	p, err := s.Get(ctx, "legacy-plc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.RetentionDays != 0 || len(p.Tags) != 1 || p.Tags[0].RetentionDays != 0 {
		t.Fatalf("legacy retention = plc %d, tags %+v; want 0 and one tag with 0", p.RetentionDays, p.Tags)
	}
	if p.Address != "10.0.0.1" || p.Slot != 2 || p.ScanRate != "500ms" {
		t.Errorf("legacy PLC fields not preserved: %+v", p)
	}
	if tag := p.Tags[0]; tag.Name != "OldTag" || !tag.Writable || !tag.DCMDEnabled || tag.Deadband.Absolute != 0.5 {
		t.Errorf("legacy tag fields not preserved: %+v", tag)
	}
	_ = s.Close()

	s2, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open (idempotency): %v", err)
	}
	_ = s2.Close()
}
