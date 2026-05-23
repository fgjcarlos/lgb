# ADR-0001: CLI framework — Cobra v1.10.2

**Date**: 2026-05-23
**Status**: Proposed

## Decision

Use `github.com/spf13/cobra` v1.10.2 as the CLI framework for `cmd/lgb`. The command tree is rooted at `cmd/lgb/cmd/root.go` with subcommands `server`, `version`, `status`, `doctor`, and `config validate`. All commands accept dependency injection via a `Deps` struct — no package-level state.

## Context

LGB requires a hierarchical command tree with a persistent pre-run hook (config loading + logger initialisation) that runs before every subcommand. The binary must compile with `CGO_ENABLED=0` on four target platforms. The CLI must be testable in unit tests without spawning a subprocess (subcommand `RunE` is extracted into a pure function accepting `io.Writer` parameters).

## Options Considered

| Option | Pros | Cons |
|--------|------|------|
| `github.com/spf13/cobra` v1.10.2 | Mature; nested subcommands; `PersistentPreRunE`; ecosystem (kubectl, Hugo, GitHub CLI); pure-Go | Adds an external dependency |
| `github.com/urfave/cli/v3` | Lightweight; composable | Less ergonomic for deeply nested `config validate`; smaller ecosystem |
| `stdlib flag` | Zero dependency | No nested subcommands; no persistent pre-run hook; would require hand-rolling framework |

## Rationale

The `config validate` subcommand (`lgb config validate --config path`) requires two levels of nesting. Cobra's `PersistentPreRunE` hook is idiomatic for config + logger wiring that must precede every leaf command. The Cobra ecosystem (k8s, Hugo, gh) means LGB contributors will already know the patterns. The library is pure-Go and verified `CGO_ENABLED=0` compatible.

## Consequences

- **Accepted**: One external runtime dependency (`spf13/cobra` + `spf13/pflag`). Both are pure-Go.
- **Monitor**: Cobra major-version changes (v2 under discussion). Current v1.10.2 is stable.
- **Revisit**: If the command tree grows beyond ~20 commands, consider whether `urfave/cli/v3` plugin model is more ergonomic.

## References

- Spec: MVP-FND-1.1, MVP-FND-1.2–1.6
- Design: §2 (component graph), §6.1–6.4, decision #1
- Upstream: https://github.com/spf13/cobra
