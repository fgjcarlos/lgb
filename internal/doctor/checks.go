// checks.go — Phase-0 diagnostic check implementations.
//
// Each check is an unexported struct implementing the Check interface.
// Tests register fakes via the same interface for isolation.
//
// Requirements: MVP-FND-8.2. Design: §10.
package doctor

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/datadir"
)

// dataDirCheck verifies the data directory exists and is writable.
type dataDirCheck struct {
	cfg *config.Config
}

func (c *dataDirCheck) Name() string { return "data-dir-writable" }

func (c *dataDirCheck) Run(ctx context.Context) Result {
	path, err := datadir.Resolve(c.cfg, "")
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("resolve data dir: %v", err),
		}
	}
	if _, err := datadir.Ensure(path); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("%v", err),
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("%s is writable", path),
	}
}

// resticCheck verifies that the restic binary is on PATH.
type resticCheck struct{}

func (c *resticCheck) Name() string { return "restic-on-path" }

func (c *resticCheck) Run(ctx context.Context) Result {
	if _, err := exec.LookPath("restic"); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarn,
			Message: "restic not found on $PATH — backup checks unavailable",
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "restic found on $PATH",
	}
}

// goRuntimeCheck reports the running Go version as informational.
type goRuntimeCheck struct{}

func (c *goRuntimeCheck) Name() string { return "go-runtime-version" }

func (c *goRuntimeCheck) Run(ctx context.Context) Result {
	v := runtime.Version() // e.g. "go1.24.0"
	msg := fmt.Sprintf("runtime version: %s", v)

	// Parse major.minor to determine if >= 1.24.
	trimmed := strings.TrimPrefix(v, "go")
	parts := strings.SplitN(trimmed, ".", 3)
	if len(parts) >= 2 {
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		if major > 1 || (major == 1 && minor >= 24) {
			return Result{Name: c.Name(), Status: StatusPass, Message: msg}
		}
	}
	return Result{Name: c.Name(), Status: StatusInfo, Message: msg + " (< 1.24 recommended)"}
}

// portCheck verifies that the configured HTTP address is not already in use.
type portCheck struct {
	cfg *config.Config
}

func (c *portCheck) Name() string { return "http-port-available" }

func (c *portCheck) Run(ctx context.Context) Result {
	addr := c.cfg.Server.HTTPAddr
	if addr == "" {
		addr = ":8080"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("%s is already in use: %v", addr, err),
		}
	}
	_ = ln.Close()
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("%s is available", addr),
	}
}

// configLoadedCheck always passes when reached (config was loaded by PersistentPreRunE).
type configLoadedCheck struct{}

func (c *configLoadedCheck) Name() string { return "config-loaded" }

func (c *configLoadedCheck) Run(ctx context.Context) Result {
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "configuration loaded and valid",
	}
}

