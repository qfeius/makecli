# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Features

- **update**: Add `--check` to report whether an update is available without installing
- **config**: Backend dev/test/production environment presets + host-only `--meta-server-url` (#14)
- **app**: Merge `app init` into `app create` — one command scaffolds the project, creates the remote App, and prepares code repositories (#15)

### Documentation

- Add `CHANGELOG.md`

## [v0.3.4] - 2026-06-15

### Features

- **login**: Browser OAuth login command — PKCE, single-hop discovery, RFC 7591 dynamic registration, ephemeral-port loopback callback
- **login**: Derive the OAuth server from the per-profile `auth-server-url`
- **deploy**: Add `app deploy` backed by the code repository service
- **preflight**: Add `preflight` to validate the app project layout
- **record**: Add `--filter` (raw CEL) to `record list`

### Bug Fixes

- **login**: Request `make:resources` scope instead of `mcp:*` scopes
- **login**: Ignore stray empty callback requests to prevent state mismatch

### Refactor

- **oauth**: Non-blocking callback send, single-switch `OpenBrowser`, test hardening

## [v0.3.3] - 2026-06-11

### Bug Fixes

- **filter**: Send the list filter as a CEL `Expression` object

## [v0.3.2] - 2026-06-02

### Features

- Add the runtime build contract to the app init template

### CI

- Skip Claude review/security jobs on fork PRs (#13)

## [v0.3.1] - 2026-05-30

### Features

- **update**: Verify the release checksum and harden the atomic install
- **notifier**: Background update-check notifier with an atomic cache and tri-state toggle
- **config**: `[settings]` global section with `LoadSettings`
- **build**: Derive `Date` from `vcs.time` when not injected via ldflags

### Bug Fixes

- **update**: Support Windows zip assets and bound stalled downloads
- **apply**: Distinguish not-found from transport errors via `api.ErrNotFound`
- **diff**: Exit non-zero on drift in JSON mode without swallowing errors
- **config**: Write config and credentials atomically; preserve `[settings]` across round-trip
- **notifier**: Suppress the notice on prerelease/dev builds; back off on refresh failure

### Refactor

- **api**: Collapse Entity/Relation writes into `writeResource` + `metaVersion`
- **cmd**: Use `slices.Contains` for the manifest extension check; add a `defaultName` helper

## [v0.3.0] - 2026-05-18

### ⚠ Breaking Changes

- Adopt the key/name DSL schema split (AgenticDSL MR #38)

## Earlier

Releases before v0.3.0 (v0.1.x–v0.2.x) predate this changelog. See the
[GitHub releases](https://github.com/qfeius/makecli/releases) for their notes.

[Unreleased]: https://github.com/qfeius/makecli/compare/v0.3.4...HEAD
[v0.3.4]: https://github.com/qfeius/makecli/releases/tag/v0.3.4
[v0.3.3]: https://github.com/qfeius/makecli/releases/tag/v0.3.3
[v0.3.2]: https://github.com/qfeius/makecli/releases/tag/v0.3.2
[v0.3.1]: https://github.com/qfeius/makecli/releases/tag/v0.3.1
[v0.3.0]: https://github.com/qfeius/makecli/releases/tag/v0.3.0
