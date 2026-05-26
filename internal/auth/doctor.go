package auth

import (
	"context"
	"fmt"

	"github.com/fgjcarlos/lgb/internal/doctor"
)

type usersExistCheck struct {
	store *UserStore
}

func NewUsersExistCheck(store *UserStore) doctor.Check {
	return &usersExistCheck{store: store}
}

func (c *usersExistCheck) Name() string { return "auth-users-exist" }

func (c *usersExistCheck) Run(ctx context.Context) doctor.Result {
	count, err := c.store.Count(ctx)
	if err != nil {
		return doctor.Result{
			Name:    c.Name(),
			Status:  doctor.StatusFail,
			Message: fmt.Sprintf("cannot query user store: %v", err),
		}
	}
	if count == 0 {
		return doctor.Result{
			Name:    c.Name(),
			Status:  doctor.StatusWarn,
			Message: "no users configured — set LGB_AUTH_ADMIN_PASSWORD to create admin on first run",
		}
	}
	return doctor.Result{
		Name:    c.Name(),
		Status:  doctor.StatusPass,
		Message: fmt.Sprintf("%d user(s) configured", count),
	}
}
