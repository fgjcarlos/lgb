//go:build !no_embed

// embed.go — embeds the compiled frontend assets into the lgb package.
//
// This file is compiled when the `no_embed` build tag is NOT set. It embeds
// the entire frontend/dist/ directory into the binary, making the web UI
// available without external files at runtime.
//
// In Phase 0 the build pipeline (Makefile + CI) passes `-tags no_embed` so
// the frontend/dist directory is not required during development builds.
// This guard MUST be removed before this change is archived so that
// production builds embed the frontend assets.
//
// TODO(archive): remove the !no_embed guard and the noassets.go counterpart
// before archiving this change (per spec MVP-FND-1.10).
//
// Requirements: MVP-FND-1.10, MVP-FND-9.6. Design: §24 (embed guard risk).
package lgb

import "embed"

// Assets holds the compiled frontend files from frontend/dist/.
// Use //go:embed all:frontend/dist to include every file regardless of
// whether its name starts with "." or "_" (Go's embed convention).
//
//go:embed all:frontend/dist
var Assets embed.FS
