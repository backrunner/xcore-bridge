# Project Instructions

## Project Snapshot

`xcore-bridge` is a macOS-only Go CLI for Surge for Mac. It embeds `xray-core` and generates Surge External Proxy entries for VLESS links.

Surge owns the managed proxy process lifecycle: a managed policy starts one foreground `xcore-bridge run` process, that process hosts the local xray-core SOCKS5 inbound, and it stays alive until Surge stops it. Do not replace the generated policies with plain SOCKS5 entries or move xray-core into an independent daemon without first re-evaluating Surge External Proxy DIRECT-routing semantics.

The supported release artifacts are only:

- `darwin/amd64`
- `darwin/arm64`

Linux release binaries and Linux installers are intentionally unsupported because Surge for Mac is required.

## User-Facing Behavior

- `add` can run without `--profile`; `remove` accepts managed policy names to delete.
- When no profile is supplied, it discovers Surge `.conf` files from iCloud Drive first, then local Surge profiles.
- The first write to a profile requires user confirmation unless `--yes` is passed.
- Every actual profile write must create or replace exactly one adjacent backup at `profile.conf.bak`.
- `add` and `remove` must only modify the managed block inside the `[Proxy]` section.
- Generated Surge External Proxy policies must connect to the local xray-core SOCKS5 inbound and enable UDP relay.
- The external process must be long-lived per active managed policy; it must not exit after the first connection.
- Startup readiness must verify a real SOCKS5 no-auth handshake, not only that the TCP port accepts connections.
- Generated policies must avoid existing policy names, profile-used local ports, and currently occupied `127.0.0.1` TCP or UDP ports.
- VLESS share links are sensitive because generated Surge profile lines contain the full link as process arguments.

## Development Standards

- Keep the codebase small and direct; prefer existing local helpers over new abstractions.
- Keep files organized by responsibility. Split large single files into focused modules in the package that owns the behavior instead of letting command, profile editing, discovery, parsing, and file I/O logic accumulate together.
- Use Go standard library APIs for filesystem, path, and parsing work where possible.
- Keep tests focused on behavior that protects user profiles: discovery, confirmation, backup, section editing, and port/name conflict handling.
- Run `gofmt` on changed Go files.
- Before committing or calling a change CI-ready, run the same checks as CI: `test -z "$(gofmt -l .)"`, `sh -n scripts/*.sh`, `go test ./...`, and darwin release builds for `darwin/amd64` and `darwin/arm64`.
- Keep README user-focused and prune stale release or platform instructions when behavior changes.

## Commit Messages

Commit messages must follow:

```text
xxx: (comp) desc
```

Where:

- `xxx` is the change type, such as `feat`, `fix`, `docs`, `test`, `chore`, or `ci`.
- `comp` is the component, such as `surge`, `install`, `release`, `docs`, or `ci`.
- `desc` is a short imperative or descriptive summary.

Examples:

```text
feat: (surge) auto-discover macOS profiles
fix: (install) require single profile backup
docs: (readme) trim obsolete release notes
```

## Release Notes

- Tags beginning with `v` trigger the GitHub Actions release workflow.
- Tags containing `-` are prereleases/beta releases.
- The installer defaults to the latest stable release and falls back to the newest prerelease only when no stable release exists.
