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
		err := s.bkpMgr.BackupAll(context.Background(), nil)
		s.bkpStatus.mu.Lock()
		defer s.bkpStatus.mu.Unlock()
		if err != nil {
			s.bkpStatus.status = "failed"
			s.bkpStatus.lastErr = err.Error()
		} else {
			s.bkpStatus.status = "idle"
			s.bkpStatus.lastRun = time.Now()
			s.bkpStatus.lastErr = ""
		}
	}()

	writeJSON(w, http.StatusAccepted, struct {
		Status string `json:"status"`
	}{Status: "started"})
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

