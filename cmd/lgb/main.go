// cmd/lgb is the LGB gateway binary entry point.
//
// Build metadata is injected at build time via ldflags. The Makefile target:
//
//	go build -ldflags "$(LDFLAGS)" -o bin/lgb ./cmd/lgb
//
// where LDFLAGS is:
//
//	-X github.com/fgjcarlos/lgb/internal/version.Version=$(VERSION)
//	-X github.com/fgjcarlos/lgb/internal/version.Commit=$(COMMIT)
//	-X github.com/fgjcarlos/lgb/internal/version.Date=$(DATE)
//
// The Cobra command tree is wired in cmd/lgb/cmd/root.go.
// Requirements: MVP-FND-1.1, MVP-FND-1.7. Design: §6.1–6.4.
package main

import (
	"fmt"
	"os"

	"github.com/fgjcarlos/lgb/cmd/lgb/cmd"
)

func main() {
	root, d := cmd.NewRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		code := cmd.ExitCode(err)
		if d.Exit != nil {
			d.Exit(code)
		} else {
			os.Exit(code)
		}
	}
}
