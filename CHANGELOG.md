# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.5.3] - 2026-07-02

### Bug Fixes

- **config**: Point the `production` environment preset to `qfei.cn` (Meta/Repo/Auth host triplet). The `dev` and `test` presets stay on `qtech.cn`; only production moved. A default-configured CLI now talks to `make.qfei.cn` out of the box

## [v0.5.2] - 2026-07-01

### Features

- **apply, diff**: Add `--max-depth` to control directory recursion (default `2`): `1` scans only the given directory (the previous behavior), `2` also descends into immediate subdirectories, `0` recurses the entire tree. Directory loading moved from single-level `os.ReadDir` to `filepath.WalkDir`; hidden files and hidden subdirectories (`.git` etc.) are never descended. `diff` shares the same flag so `diff` and `apply` agree on which files constitute an app

## [v0.5.1] - 2026-07-01

### Features

- **configure**: Add `configure resolve --target local-preview` — a token-free, offline command that resolves the active profile / environment / overrides into the minimal JSON (`make_api_origin`, `tenant_id`, `operator_id`) the Make platform skills need to wire up a local preview backend, instead of tooling having to re-derive the origin from scattered config (#20)
- **scaffold**: Sharpen the generated `AGENTS.md` guidance around `make app` local preview — clearer formal-login steps, local-preview auth handling, and environment resolution so a freshly scaffolded app points its preview at the right backend (#20)

### Features

- **config**: The backend environment now defaults to `production` (previously had no default), so a freshly configured CLI talks to the production platform out of the box. Override per-call with `--env dev|test|production` or persist it via `[settings] environment`
- **entity**: Support Entity-level unique constraints (`properties.uniqueConstraints`), matching the AgenticDSL update. The `entity create --json` file now carries the whole entity `properties` (`fields` + `uniqueConstraints`) — the same shape as the DSL YAML and `entity list -o json`'s `data.properties`, so the three round-trip. `apply` round-trips constraints from YAML; `diff` detects drift by constraint name with order-sensitive field comparison; `entity list <key>` renders a unique-constraints table in the detail view. The whitelist of constraint-eligible field types, the per-entity quota, and existing-duplicate rejection are enforced server-side
- **record**: Writes (`record create` / `record update`) that violate a unique constraint now surface a friendly `UniqueConstraintError` naming the conflicting constraint and fields, instead of the raw backend message
- **errors**: Authentication failures are now upgraded to an actionable message that guides you to run `makecli login`, echoing the active profile and environment

## [v0.4.6] - 2026-06-18

### Features

- **dry-run**: Add `--dry-run` to the create commands (`app` / `entity` / `relation` / `record create`), injecting an `X-Dry-Run` header so the backend runs the real validation but rolls back instead of persisting — answers "would this create succeed?" with no side effects. `app create --dry-run` short-circuits before any local scaffold, `git init`, or repo prep; the header is only sent on write operations, never on reads

### Refactor

- **preflight**: Rename the `--type` flag to `--app-type`

## [v0.4.5] - 2026-06-17

### Features

- **deploy**: `--env` now defaults to `preview` (no longer required); `production` must be opted into explicitly and prompts for a continue/abort confirmation before pushing — `--yes`/`-y` skips it, and non-interactive shells are refused with guidance to pass `--yes`. The internal repository clone URL is no longer printed
- **deploy**: Verify the app is registered on the Make platform before pushing. A project that was only `app init`'d (never `app create`'d) is now refused with guidance to run `makecli app create -f apps/dsl/app.yaml`, instead of silently pushing code for an unregistered app and leaving a repo-without-app orphan state
- **configure**: Add `--sample` to print a commented INI reference template, plus richer command examples so config keys are discoverable

## [v0.4.4] - 2026-06-17

### ⚠ Breaking Changes

- **deploy**: No longer auto-commits a worktree snapshot before pushing. `app deploy` now pushes the committed HEAD as-is and errors if the working tree is dirty, has no commits, or has no git repository — guiding you to commit first. Commit timing is handed back to the user

### Features

- **app**: Re-introduce `app init [appKey]` — scaffolds a complete local Make app project (`CLAUDE.md` / `AGENTS.md` / `apps/dsl/app.yaml` + `git init` + `.gitignore`), idempotent and remote-free. `app create` now shares the same scaffold core (`create` = `init` + remote registration + initial commit) and composes with a pre-existing `init` scaffold without clobbering edits
- **api**: Inject W3C `traceparent` + `X-Log-Id` headers on outbound requests for distributed tracing
- **login**: Add lark-cli-style progress messages to the browser OAuth flow

### Bug Fixes

- **deploy**: `gitSignature` reads the merged git config (LocalScope) so commits use the real user identity from `~/.gitconfig`, instead of only `/etc/gitconfig` (which almost always fell back to the `makecli` identity)

## [v0.4.3] - 2026-06-16

### Features

- **preflight**: Add `--type` flag (`fullstack`/`service`/`ui`, default `fullstack`) so headless and ui-only apps can gate CI/deploy without failing on absent siblings; `apps/dsl` stays required across all types

## [v0.4.2] - 2026-06-16

### Features

- **app**: Confirm before `app delete` by typing the app key (gh `repo delete` style), with `--yes`/`-y` to skip; non-interactive shells are refused with guidance to pass `--yes`. Requires Go 1.25.8+ to build (huh v2)

## [v0.4.1] - 2026-06-16

### ⚠ Breaking Changes

- **deploy**: Pure-Go git via go-git — no longer shells out to the system `git`; the binary is now self-contained. Reads the app key from `apps/dsl/app.yaml` (single source of truth, no `--app` flag) and auto-commits a worktree snapshot before pushing

### Features

- **app**: Add a DESCRIPTION column to the `app list` table (sourced from app properties)
- **update**: Sync Make platform skills during update (#17)

## [v0.4.0] - 2026-06-16

### ⚠ Breaking Changes

- **app**: Merge `app init` into `app create` — one command scaffolds the project, creates the remote App, and prepares code repositories; the standalone `app init` command is removed (#15)

### Features

- **update**: Add `--check` to report whether an update is available without installing (#16)
- **config**: Backend dev/test/production environment presets + host-only `--meta-server-url` (#14)

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

[Unreleased]: https://github.com/qfeius/makecli/compare/v0.5.3...HEAD
[v0.5.3]: https://github.com/qfeius/makecli/releases/tag/v0.5.3
[v0.5.2]: https://github.com/qfeius/makecli/releases/tag/v0.5.2
[v0.5.1]: https://github.com/qfeius/makecli/releases/tag/v0.5.1
[v0.5.0]: https://github.com/qfeius/makecli/releases/tag/v0.5.0
[v0.4.6]: https://github.com/qfeius/makecli/releases/tag/v0.4.6
[v0.4.5]: https://github.com/qfeius/makecli/releases/tag/v0.4.5
[v0.4.4]: https://github.com/qfeius/makecli/releases/tag/v0.4.4
[v0.4.3]: https://github.com/qfeius/makecli/releases/tag/v0.4.3
[v0.4.2]: https://github.com/qfeius/makecli/releases/tag/v0.4.2
[v0.4.1]: https://github.com/qfeius/makecli/releases/tag/v0.4.1
[v0.4.0]: https://github.com/qfeius/makecli/releases/tag/v0.4.0
[v0.3.4]: https://github.com/qfeius/makecli/releases/tag/v0.3.4
[v0.3.3]: https://github.com/qfeius/makecli/releases/tag/v0.3.3
[v0.3.2]: https://github.com/qfeius/makecli/releases/tag/v0.3.2
[v0.3.1]: https://github.com/qfeius/makecli/releases/tag/v0.3.1
[v0.3.0]: https://github.com/qfeius/makecli/releases/tag/v0.3.0
