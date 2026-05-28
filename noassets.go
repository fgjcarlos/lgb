//go:build no_embed

// noassets.go — stub companion to embed.go for `-tags no_embed` builds.
//
// When the `no_embed` build tag is active, embed.go is excluded and this file
// is compiled instead. It declares the same package and exposes an empty
// embed.FS so packages that reference `lgb.Assets` (notably internal/server)
// compile and run with the SPA-mount path becoming a runtime no-op.
//
// Requirements: MVP-FND-1.10, MVP-FND-9.6. Design: §24 (embed guard risk).
package lgb

import "embed"

// Assets is an empty embed.FS for no_embed builds. mountSPA detects the empty
// tree at runtime and skips installing the SPA catch-all route.
var Assets embed.FS
