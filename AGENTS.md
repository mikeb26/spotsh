# AGENTS.md

## Project overview
Spot Shell (spotsh) is a Go CLI that launches and connects to an AWS EC2 **spot** instance to provide a disposable “shell box”. It uses the AWS SDK for Go v2 and includes commands like `launch`, `ssh`, `scp`, `terminate`, `price`, `vpn`, and `image`.

## Repository layout
- `.github/ISSUE_TEMPLATE/`: GitHub issue forms for bugs, feature requests, and documentation.
- `cmd/spotsh/`: CLI entrypoint and subcommands. Embeds `help.txt`, `version.txt`, and VPN setup/teardown scripts.
- `aws/`: AWS-facing logic (EC2, SSM, IAM, VPC/SG, pricing).
- `types.go`: Shared types (e.g., supported OS list).
- `.circleci/`: CI build/test/release pipeline.

## Setup / build commands
Prereqs:
- Go: see `go.mod` (currently `go 1.22.3`).
- This repo expects vendored dependencies (Makefile sets `GOFLAGS=-mod=vendor`).

Common commands:
- Vendor deps: `make vendor`
- Build binary: `make build` (or just `make`)
- Run tests: `make test`
- Clean: `make clean`

## Testing instructions (important: AWS integration tests)
Most tests under `./aws` are **integration tests** that call real AWS APIs and may create/modify resources.

- Minimal local unit tests only (no AWS calls):
  - `go test .`

- Full test suite (requires AWS credentials + may create resources):
  - `go test ./...` (or `make test`)

Running the full suite may:
- Create/delete EC2 key pairs.
- Query/modify VPC/security group rules.
- Query SSM parameters for AMI IDs.
- Launch/terminate spot instances.

Requirements for full tests:
- AWS credentials available via standard mechanisms (e.g., `~/.aws/config`, env vars, SSO, etc.).
- An AWS region configured (same default as `aws configure`).
- Expect failures in sandboxed/CI environments without AWS credentials/permissions.

CI-specific:
- CircleCI runs `make vendor`, `make build`, and `make unit-tests.xml`.
- `make unit-tests.xml` requires `aws` CLI and `gotestsum` and interacts with AWS.

Agent guidance:
- Do **not** run AWS integration tests unless the user explicitly wants it and understands the cost/side effects.
- If you need quick validation in restricted environments, use `go test .` plus `gofmt`/compile checks.

## Keeping AGENTS.md up to date
- Update this `AGENTS.md` whenever you change workflows, repo layout, build/test commands, CI behavior, or any guidance an agent would rely on.

## Code style / conventions
- Run `gofmt` on any modified `.go` files.
- Prefer small, targeted changes; avoid introducing new dependencies unless necessary.
- Keep CLI UX stable:
  - Flag names and help text are user-facing.
  - `cmd/spotsh/help.txt` is embedded and should be updated if command semantics change.

## Security / operational safety
- Never log or exfiltrate AWS credentials, account IDs, or other secrets.
- Be cautious with changes that affect resource creation/deletion (EC2, IAM, SG rules, AMIs) and anything that could increase cost.
- The VPN feature shells out to local and remote commands (WireGuard/SSH). Treat embedded scripts as security-sensitive.

## Release notes
- Releases are handled in CircleCI and use `git describe --tags` to populate `cmd/spotsh/version.txt` for tagged builds.
