package server

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// backupStatus tracks the state of the most recent backup operation.
// All fields are protected by mu.
type backupStatus struct {
	mu      sync.Mutex
	status  string    // "idle" | "running" | "failed"
	lastRun time.Time // zero when no backup has ever completed
	lastErr string
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
func (s *Server) recordBackupResult(err error, manual bool) {
	s.bkpStatus.mu.Lock()
	defer s.bkpStatus.mu.Unlock()
	if err != nil {
		s.log.Error("backup run failed", "component", "backup", "manual", manual, "err", err)
		s.bkpStatus.lastErr = err.Error()
	} else {
		s.bkpStatus.lastRun = time.Now()
		s.bkpStatus.lastErr = ""
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
	s.bkpStatus.mu.Unlock()

	resp := struct {
		Status  string  `json:"status"`
		LastRun *string `json:"last_run"`
		LastErr string  `json:"last_error,omitempty"`
	}{
		Status:  status,
		LastErr: lastErr,
	}
	if !lastRun.IsZero() {
		s := lastRun.UTC().Format(time.RFC3339)
		resp.LastRun = &s
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
