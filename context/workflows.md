# Project Workflows

## Branches and review

- Implementation base and default branch: `development`.
- Promotion path: `development → staging → main` through review.
- Do not directly push feature changes to environment branches after bootstrap.

## Quality gates

Run focused RED/GREEN tests for each behavior, then `go test ./...`, `go test -race ./...`, `go vet ./...`, `pnpm.cmd --dir dashboard run verify`, `bash scripts/verify-no-live-trading-deps.sh`, `bash tests/verify-compose-topology.sh`, `bash tests/verify-compose-topology_test.sh`, and `git diff --check`. Never run `docker compose config` in a credential-bearing checkout because dotenv interpolation can expose ignored values. CI and normal non-Git-Bash shells use `pnpm`.

## Local runtime and recovery

Reuse the tracked `supabase/config.toml` and migrations; do not reinitialize the database identity after a repository rename. See `../docs/local-setup.md` for standalone setup. Start only this project’s Supabase stack; stop another project’s stack first without `--no-backup`. Do not retain `supabase status` output because it contains credentials. Rebuild the schema from migrations and sanitized seed data before integration checks.

A fresh clone initializes submodules, restores `.env.local` only from the owner’s secure store, installs exact Go and pnpm lock dependencies, initializes the project-owned Supabase config, applies migrations, and runs the documented gates. Recovery replays pending paper-position state idempotently; it never creates a live trade.

## Evidence and delivery

Record test/build/container output and sanitized runtime probes. Distinguish local compiled/tested, locally runtime-verified, remotely CI-verified, and credential-blocked states. Runtime logs/reports are local ignored data, not delivery artifacts.
