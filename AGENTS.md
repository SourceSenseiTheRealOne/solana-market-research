# Solana Market Research Agent Rules

## Context and boundaries

1. Read `context/README.md`, then the relevant linked context before making changes.
2. Run `codegraph status .` before investigation or edits; use CodeGraph before broad source search. Sync after source-path changes and never commit `.codegraph/`.
3. This project is personal, public source with local-only runtime data. Do not read or commit `.env.local`, Supabase local state, reports, logs, credentials, provider payloads, or Hermes private state.

## Safety invariants

- This is a paper-trading research bot only. Never add wallet packages, seed/private-key handling, signing, transaction construction, swap execution, Trigger orders, blockchain writes, or a browser-to-Supabase path.
- The dashboard is read-only and loopback-only. The Go API is its sole data source.
- Persist financial values as minor-unit integers or decimal strings, never `float64`.
- Use UTC for timestamps and quota buckets.
- Ent owns typed Go queries; reviewed Atlas-generated SQL under `supabase/migrations/` is the only migration history.

## Engineering workflow

- Behavioral changes use strict RED → GREEN → REFACTOR, with a real observed failing test before production code.
- Preserve the layered dependency direction: domain → application → ports → adapters/transport. Domain and application must not import framework or provider clients.
- Keep HTTP clients bounded: explicit timeouts, pagination limits, response-size limits, validation, idempotency, and redacted structured `slog` fields.
- Never commit, push, reset, clean, or stash unless the owner explicitly requests it.

## Required local gates

```bash
go test ./...
go test -race ./...
go vet ./...
pnpm --dir dashboard run verify
bash scripts/verify-no-live-trading-deps.sh
bash tests/verify-compose-topology.sh
bash tests/verify-compose-topology_test.sh
git diff --check
```

On this Windows Git-Bash host, use `pnpm.cmd` in place of `pnpm` because the Corepack shell shim is broken; CI and normal shells use `pnpm`.
