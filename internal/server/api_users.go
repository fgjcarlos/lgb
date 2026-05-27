package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// userResponse is the API-safe user representation. It deliberately omits
// PasswordHash to avoid exposing credentials in any endpoint response.
type userResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      auth.Role `json:"role"`
	CreatedAt string    `json:"created_at"`
}

func userToResponse(u *auth.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// handleListUsers returns a paginated list of users.
// GET /api/users  (admin only)
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r)
	if !ok {
		return
	}

	users, err := s.userStore.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not list users")
		return
	}

	count := len(users)
	start := offset
	if start > count {
		start = count
	}
	end := start + limit
	if end > count {
		end = count
	}

	resp := make([]userResponse, 0, end-start)
	for _, u := range users[start:end] {
		u := u
		resp = append(resp, userToResponse(&u))
	}

	writeJSON(w, http.StatusOK, struct {
		Data       []userResponse     `json:"data"`
		Pagination paginationResponse `json:"pagination"`
	}{
		Data:       resp,
		Pagination: paginationResponse{Limit: limit, Offset: offset, Count: count},
	})
}

// handleCreateUser creates a new user.
// POST /api/users  (admin only)
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		Role     auth.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "username, password, and role are required")
		return
	}
	if !req.Role.Valid() {
		writeAPIError(w, http.StatusBadRequest, "invalid_role", "role must be admin, operator, or viewer")
		return
	}

	user, err := s.userStore.Create(r.Context(), req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, auth.ErrUserAlreadyExists) {
			writeAPIError(w, http.StatusConflict, "duplicate_username", "a user with that username already exists")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not create user")
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		Data userResponse `json:"data"`
	}{Data: userToResponse(user)})
}

// handleGetUser returns a single user by ID.
// GET /api/users/{id}  (admin only)
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}

	user, err := s.userStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeAPIError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not get user")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Data userResponse `json:"data"`
	}{Data: userToResponse(user)})
}

// handleUpdateUserRole updates the role of a user.
// PUT /api/users/{id}/role  (admin only)
func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Role auth.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}
	if !req.Role.Valid() {
		writeAPIError(w, http.StatusBadRequest, "invalid_role", "role must be admin, operator, or viewer")
		return
	}

	if err := s.userStore.UpdateRole(r.Context(), id, req.Role); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeAPIError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update role")
		return
	}

	user, err := s.userStore.GetByID(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not retrieve updated user")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Data userResponse `json:"data"`
	}{Data: userToResponse(user)})
}

// handleUpdateUserPassword resets the password of a user.
// PUT /api/users/{id}/password  (admin only)
func (s *Server) handleUpdateUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "password is required")
		return
	}

	if err := s.userStore.UpdatePassword(r.Context(), id, req.Password); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeAPIError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update password")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteUser deletes a user, guarding against removal of the last admin.
// DELETE /api/users/{id}  (admin only)
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}

	ctx := r.Context()

	// Fetch the target user to know their role before deletion.
	user, err := s.userStore.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeAPIError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not get user")
		return
	}

	// Last-admin guard: if the target is an admin, ensure there is more than one.
	if user.Role == auth.RoleAdmin {
		adminCount, err := s.userStore.CountByRole(ctx, auth.RoleAdmin)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not count admins")
			return
		}
		if adminCount <= 1 {
			writeAPIError(w, http.StatusConflict, "last_admin", "cannot delete the last admin user")
			return
		}
	}

	if err := s.userStore.Delete(ctx, id); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeAPIError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseUserID extracts and validates the {id} path parameter.
func parseUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "id must be a positive integer")
		return 0, false
	}
	return id, true
}
