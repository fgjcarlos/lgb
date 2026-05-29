// embed.go — embeds the compiled frontend assets into the lgb package.
//
// `//go:embed all:frontend/dist` requires the directory to exist at compile
// time. A committed `frontend/dist/.gitkeep` placeholder satisfies that
// requirement for fresh clones and backend-only iteration; `make build-with-ui`
// (and the CI `frontend-build` job) populate `dist/` with the real bundle
// before the Go build embeds it.
//
// Runtime mounting is no-op-safe: `mountSPA` (internal/server/spa.go) detects
// the missing `index.html` and skips installing the SPA catch-all route, so a
// backend-only binary still serves the JSON API normally.
//
// Requirements: MVP-FND-1.10, MVP-FND-9.6. Design: §24 (embed guard risk).
package lgb

import "embed"

// Assets holds the compiled frontend files from frontend/dist/.
//
//go:embed all:frontend/dist
var Assets embed.FS
