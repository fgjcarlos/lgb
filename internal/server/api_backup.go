package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/backup"
)

// backupStatus tracks the state of the most recent backup operation.
// All fields are protected by mu.
type backupStatus struct {
	mu      sync.Mutex
	status  string    // "idle" | "running" | "failed"
	lastRun time.Time // zero when no backup has ever completed
	lastErr string
	restore restoreState
}

// restoreState tracks the most recent restore run (guarded by backupStatus.mu).
type restoreState struct {
	status      string // "" (idle) | "running" | "succeeded" | "failed"
	snapshotID  string
	destination string
	finishedAt  time.Time
	lastErr     string
}

// handleBackupRestore validates a restore request, audits it and restores the
// snapshot asynchronously into an empty destination directory.
// POST /api/backup/restore  (admin only)
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req struct {
		Repo        int    `json:"repo"`
		SnapshotID  string `json:"snapshot_id"`
		Destination string `json:"destination"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := s.bkpMgr.CheckRestore(req.Repo, req.SnapshotID, req.Destination); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	dest := filepath.Clean(req.Destination)

	s.bkpStatus.mu.Lock()
	if s.bkpStatus.restore.status == "running" {
		s.bkpStatus.mu.Unlock()
		writeAPIError(w, http.StatusConflict, "conflict", "a restore is already running")
		return
	}
	s.bkpStatus.restore = restoreState{status: "running", snapshotID: req.SnapshotID, destination: dest}
	s.bkpStatus.mu.Unlock()

	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "backup.restore",
			Username: actorFromContext(r),
			Detail:   fmt.Sprintf("repo=%d snapshot=%s destination=%s", req.Repo, req.SnapshotID, dest),
			IP:       s.clientAddr(r),
		})
	}

	go func() {
		err := s.bkpMgr.Restore(context.Background(), req.Repo, req.SnapshotID, dest)
		s.bkpStatus.mu.Lock()
		defer s.bkpStatus.mu.Unlock()
		s.bkpStatus.restore.finishedAt = time.Now()
		s.bkpStatus.restore.status = "succeeded"
		if err != nil {
			s.log.Error("backup restore failed", "component", "backup", "snapshot", req.SnapshotID, "err", err)
			s.bkpStatus.restore.status = "failed"
			s.bkpStatus.restore.lastErr = err.Error()
		}
	}()

	writeJSON(w, http.StatusAccepted, struct {
		Status string `json:"status"`
	}{Status: "started"})
}

// handleBackupTrigger starts a backup asynchronously.
// POST /api/backup/trigger  (admin only)
func (s *Server) handleBackupTrigger(w http.ResponseWriter, r *http.Request) {
	s.bkpStatus.mu.Lock()
	if s.bkpStatus.status == "running" {
		s.bkpStatus.mu.Unlock()
		writeAPIError(w, http.StatusConflict, "conflict", "a backup is already running")
		return
	}
	s.bkpStatus.status = "running"
	s.bkpStatus.mu.Unlock()

	go func() {
		s.recordBackupResult(s.bkpMgr.BackupAll(context.Background(), nil), true)
	}()

	writeJSON(w, http.StatusAccepted, struct {
		Status string `json:"status"`
	}{Status: "started"})
}

// RecordScheduledBackup records the outcome of a scheduled backup run (wired to
// backup.Scheduler.OnResult) so it shows in GET /api/backup/status and the log.
func (s *Server) RecordScheduledBackup(err error) {
	s.recordBackupResult(err, false)
}

// recordBackupResult stores a run outcome (backup plus retention). Only the
// manual run owns the "running" state, so a scheduled result never releases it.
// last_run advances whenever every backup succeeded, even if retention failed.
func (s *Server) recordBackupResult(err error, manual bool) {
	s.bkpStatus.mu.Lock()
	defer s.bkpStatus.mu.Unlock()
	var retErr *backup.RetentionError
	if err == nil || errors.As(err, &retErr) {
		s.bkpStatus.lastRun = time.Now()
	}
	s.bkpStatus.lastErr = ""
	if err != nil {
		s.log.Error("backup run failed", "component", "backup", "manual", manual, "err", err)
		s.bkpStatus.lastErr = err.Error()
	}
	if manual || s.bkpStatus.status != "running" {
		s.bkpStatus.status = "idle"
		if err != nil {
			s.bkpStatus.status = "failed"
		}
	}
}

// handleBackupStatus returns the current backup status.
// GET /api/backup/status  (admin only)
func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	s.bkpStatus.mu.Lock()
	status := s.bkpStatus.status
	lastRun := s.bkpStatus.lastRun
	lastErr := s.bkpStatus.lastErr
	rs := s.bkpStatus.restore
	s.bkpStatus.mu.Unlock()

	type restoreResponse struct {
		Status      string  `json:"status"`
		SnapshotID  string  `json:"snapshot_id,omitempty"`
		Destination string  `json:"destination,omitempty"`
		FinishedAt  *string `json:"finished_at,omitempty"`
		Error       string  `json:"error,omitempty"`
	}
	resp := struct {
		Status  string          `json:"status"`
		LastRun *string         `json:"last_run"`
		LastErr string          `json:"last_error,omitempty"`
		Restore restoreResponse `json:"restore"`
	}{
		Status:  status,
		LastErr: lastErr,
		Restore: restoreResponse{Status: "idle", SnapshotID: rs.snapshotID, Destination: rs.destination, Error: rs.lastErr},
	}
	if rs.status != "" {
		resp.Restore.Status = rs.status
	}
	if !lastRun.IsZero() {
		s := lastRun.UTC().Format(time.RFC3339)
		resp.LastRun = &s
	}
	if !rs.finishedAt.IsZero() {
		s := rs.finishedAt.UTC().Format(time.RFC3339)
		resp.Restore.FinishedAt = &s
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleBackupSnapshots returns the list of restic snapshots.
// GET /api/backup/snapshots  (admin only)
func (s *Server) handleBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := s.bkpMgr.Snapshots(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not retrieve snapshots")
		return
	}

	type snapshotResponse struct {
		ID       string    `json:"id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
		Paths    []string  `json:"paths"`
	}

	data := make([]snapshotResponse, 0, len(snaps))
	for _, s := range snaps {
		data = append(data, snapshotResponse{
			ID:       s.ID,
			Time:     s.Time,
			Hostname: s.Hostname,
			Paths:    s.Paths,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		Data []snapshotResponse `json:"data"`
	}{Data: data})
}
