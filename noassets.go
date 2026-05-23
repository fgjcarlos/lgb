//go:build no_embed

// noassets.go — stub companion to embed.go for `-tags no_embed` builds.
//
// When the `no_embed` build tag is active, embed.go is excluded and this file
// is compiled instead. It declares the same package so the build succeeds even
// when frontend/dist/ does not exist (e.g. during development or CI builds
// before the frontend is compiled).
//
// Requirements: MVP-FND-1.10, MVP-FND-9.6. Design: §24 (embed guard risk).
package lgb
