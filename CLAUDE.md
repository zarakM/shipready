# ShipReady

A CLI that scans a Kubernetes cluster (and later its AWS account) for production-readiness problems and writes a scored, self-contained HTML report. Full product spec: @docs/SPEC.md

## Who it is for

CTOs and lead engineers at Series A startups who have outgrown their first cluster setup. The free scan is a lead magnet for Opscify, a DevOps agency; the report ends with a "Fix it for me" call to action.

## Hard rules (never break these)

- **Read-only.** Only `get`, `list` and `watch` against the Kubernetes API, and only `Describe*`/`List*`/`Get*` against AWS. No create, update, patch, delete or exec, ever.
- **Nothing leaves the machine.** No telemetry, no uploads, no calls to any ShipReady or third-party server. The only network traffic is to the user's own cluster and cloud APIs.
- **Never read Secret values.** Inspect Secret metadata and references only. Never print, log or write secret data, tokens or kubeconfig contents into the report.
- **Single static binary.** No runtime dependencies for the user to install.
- **Report is one HTML file.** Inline CSS and JS, no CDN links, works offline.

## Stack

- Go (latest stable), `client-go` for Kubernetes, `cobra` for the CLI, `aws-sdk-go-v2` for AWS
- `html/template` with embedded assets (`embed`) for the report
- `goreleaser` for cross-platform builds (Linux, macOS, Windows)

## Layout

```
cmd/shipready/        CLI entry point and commands
internal/checks/      one file per check, grouped by category
internal/collect/     cluster and cloud data collection (read-only clients)
internal/score/       scoring logic
internal/report/      HTML and JSON renderers, embedded templates
testdata/             fixture manifests used by tests
docs/SPEC.md          product spec and milestones
```

## Commands

```
go build ./cmd/shipready
go test ./...
go vet ./...
golangci-lint run
```

## Conventions

- Every check implements the `Check` interface in `internal/checks` and has a stable ID such as `REL-001`.
- Every check has a table-driven unit test using fake clients or fixtures in `testdata/`. No test needs a live cluster.
- A check that cannot run (missing permission, API not installed) returns `Skipped` with a reason. It never fails the whole scan.
- Finding text is written for a busy CTO: what is wrong, why it matters, how to fix it. Plain language, no jargon dumps.
- Keep dependencies minimal. Ask before adding a new one.

## How to work in this repo

- Follow the milestones in `docs/SPEC.md` in order. Do not start a later milestone until the current one builds and its tests pass.
- Before a new milestone, propose a short plan and wait for approval.
- If the spec is ambiguous, ask instead of guessing, then update `docs/SPEC.md` with the decision.
