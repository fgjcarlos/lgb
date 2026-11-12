package plcstore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/plcstore"
)

// TestDeadband_RoundTrip verifies #60: the per-tag deadband survives Create,
// Get, Update and List, and an unset deadband stays zero.
func TestDeadband_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openMemory(t)

	p := samplePLC("line1")
	p.Tags = []config.TagDef{
		{Name: "Speed", Type: "Float", Deadband: config.Deadband{Absolute: 0.5, Percent: 2}},
		{Name: "Running", Type: "Boolean"},
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "line1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := (config.Deadband{Absolute: 0.5, Percent: 2}); got.Tags[0].Deadband != want {
		t.Errorf("Tags[0].Deadband = %+v; want %+v", got.Tags[0].Deadband, want)
	}
	if got.Tags[1].Deadband != (config.Deadband{}) {
		t.Errorf("Tags[1].Deadband = %+v; want zero", got.Tags[1].Deadband)
	}

	p.Tags[0].Deadband = config.Deadband{Absolute: 3}
	if err := s.Update(ctx, "line1", p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := (config.Deadband{Absolute: 3}); list[0].Tags[0].Deadband != want {
		t.Errorf("after Update Tags[0].Deadband = %+v; want %+v", list[0].Tags[0].Deadband, want)
	}
}

// TestDeadband_LegacyDB verifies the idempotent migration adds the deadband
// columns to a DB created before #60: existing tags get a zero deadband, their
// other flags are preserved, and reopening does not error.
func TestDeadband_LegacyDB(t *testing.T) {
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
    UNIQUE(plc_id, name)
);
INSERT INTO plcs(name, address, created_at, updated_at) VALUES ('legacy-plc', '10.0.0.1', 1, 1);
INSERT INTO plc_tags(plc_id, name, type, writable, dcmd_enabled, opcua_write_enabled) VALUES (1, 'OldTag', 'Float', 1, 1, 1);`)
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
	tag := p.Tags[0]
	if tag.Deadband != (config.Deadband{}) {
		t.Errorf("legacy tag Deadband = %+v; want zero", tag.Deadband)
	}
	if !tag.Writable || !tag.DCMDEnabled || !tag.OPCUAEnabled {
		t.Errorf("legacy flags not preserved: %+v", tag)
	}
	_ = s.Close()

	s2, err := plcstore.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open (idempotency): %v", err)
	}
	_ = s2.Close()
}
