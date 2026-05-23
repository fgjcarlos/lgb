# ADR-0002: Config loader — koanf v2 with `LGB_{SECTION}_{FIELD}` env overlay

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `github.com/knadh/koanf/v2` v2.3.4 as the configuration loading library. Configuration sources are merged in priority order: compiled defaults → YAML file → environment variables with the `LGB_` prefix. Environment variable names follow the pattern `LGB_{SECTION_UPPER}_{FIELD_UPPER}` (e.g. `LGB_AUTH_JWTSECRET`). Secret fields are tagged `secret:"true"` and redacted via reflection in log output.

## Context

LGB is deployed on edge hardware (Raspberry Pi, industrial PCs) where operators may not have access to edit config files. Env-var overrides are the standard ops pattern for containerised services (12-factor). The loader must preserve camelCase YAML keys (e.g. `httpAddr`, not `httpaddr`), support hot-reload (file watcher), and never expose secret values in logs. The library must be `CGO_ENABLED=0` compatible.

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `koanf/v2` v2.3.4 | Preserves key case; modular providers; per-provider hot-reload; pure-Go | External dependency |
| `spf13/viper` | Widely used; env overlay built-in | Globally stateful; normalises keys to lowercase (breaks camelCase requirement) |
| `stdlib os` + `gopkg.in/yaml.v3` | Zero external dep | No env overlay, no hot-reload; significant boilerplate |

## Rationale

Viper's forced key normalisation to lowercase breaks the `logLevel` / `httpAddr` camelCase convention required by spec MVP-FND-2.3. koanf is modular (providers are separate packages, so unused providers carry no binary weight) and its provider chain composes cleanly with a debounce watcher. The reflection-driven env key map means new fields in `Config` automatically gain env-var support without a manual list to maintain.

## Consequences

- **Accepted**: Several external packages (`koanf/v2`, `koanf/providers/*`, `koanf/parsers/yaml`, `fsnotify`). All pure-Go.
- **Monitor**: koanf v2 is a recent rewrite; check for breaking changes on minor version bumps.
- **Revisit**: If koanf becomes unmaintained, the provider interface is narrow enough to replace with a custom stack.

## References

- Spec: MVP-FND-2.1–2.6, MVP-FND-3.1–3.2
- Design: §4.1, §5.1–5.4, decisions #2, #3, #4
- Upstream: https://github.com/knadh/koanf
