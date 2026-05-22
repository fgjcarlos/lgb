// Package version holds build-time metadata injected via ldflags.
//
// Usage in Makefile:
//
//	-X github.com/fgjcarlos/lgb/internal/version.Version=$(VERSION)
//	-X github.com/fgjcarlos/lgb/internal/version.Commit=$(COMMIT)
//	-X github.com/fgjcarlos/lgb/internal/version.Date=$(DATE)
//
// When not injected (e.g., plain `go test`), the variables fall back to safe
// human-readable defaults per MVP-FND-1.7 and design decision #25.
package version

// Version is the semantic version string, e.g. "v1.2.3" or "dev".
var Version = "dev"

// Commit is the short git commit hash, e.g. "abc1234" or "none".
var Commit = "none"

// Date is the build timestamp in RFC 3339 format, e.g. "2026-05-22T00:00:00Z" or "unknown".
var Date = "unknown"

// BuildInfo holds all build-time metadata as a value type.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Info returns a snapshot of the current build metadata.
// It is safe to call from multiple goroutines.
func Info() BuildInfo {
	return BuildInfo{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
	}
}
