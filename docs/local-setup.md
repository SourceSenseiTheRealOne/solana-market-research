# Local setup

The source can be cloned without the author's Coding Lab. Runtime setup needs Docker, the Supabase CLI, Go 1.26.2, Node.js 22 or a compatible newer release, and pnpm 10.30.1. The committed Compose file starts only the API and dashboard; it does not start PostgreSQL.

## Start without paper automation

1. Install source dependencies:

   ```sh
   go mod download
   pnpm --dir dashboard install --frozen-lockfile
   ```

2. Use the existing `supabase/config.toml`. Start this project's local Supabase stack from the repository root:

   ```sh
   supabase start
   supabase migration up --local
   ```

   Check port ownership first. Only one standard-port local Supabase project should run at a time. Do not reset a populated database, delete volumes, or stop another project's stack without its owner's approval. Startup/status output contains local credentials; keep it out of logs, screenshots, Git, and support messages.

3. Create ignored `.env.local` from `.env.example` only if `.env.local` does not already exist. Set `DATABASE_URL` using your local database credentials and keep `PAPER_AUTOMATION_ENABLED=false`. For Docker Desktop, the API container reaches a host database through `host.docker.internal`, not its own `127.0.0.1`. Provider/model credentials are not required when automation is disabled.

4. Start the application explicitly:

   ```sh
   docker compose up --build
   ```

   The documented environment file is loaded by Compose. Native `go run ./cmd/paper-bot` reads process environment variables, not `.env.local` automatically. Do not paste credentials into command history.

5. Open `http://127.0.0.1:4173/`. The API health endpoint is `http://127.0.0.1:8080/healthz`; the dashboard read endpoint is `/api/v1/dashboard`. An empty database should produce an empty state, not fabricated positions. Stopping the API or withholding its database should produce an unavailable state, not simulated success.

Compose publishes both application ports on loopback. Keep them there. No authentication is implemented, and this configuration is not suitable for a public Vercel deployment or a publicly bound container.

## Optional paper automation

Enable automation only for an approved local experiment with bounded provider usage. All positions remain virtual. The configured automation requires:

| Variable | Purpose |
| --- | --- |
| `HELIUS_API_KEY` | Read-only Solana Mainnet RPC; the service derives the provider URL |
| `TWITTERAPIIO_API_KEY` | Bounded exact-mint social searches |
| `JUPITER_API_KEY` | Read-only quote evidence |
| `HERMES_API_URL`, `HERMES_API_KEY` | A separately provisioned restricted local verdict gateway |

The adapter pins `openai-codex` / `gpt-5.6-sol`; availability and authorization must be checked by the operator. The browser receives none of these credentials. `PAPER_QUOTE_MINT` defaults to canonical Solana Mainnet USDC and remains a read-only simulation input. Do not add wallets, signing, transaction construction, or broadcast to make a paper experiment work.

## Verification without private research data

Use the explicit commands in the [README](../README.md#verification). Go tests do not load `.env.local`. Database contracts need a dedicated disposable database with the committed migrations applied and `TEST_DATABASE_URL` set in the test process. Never point that variable at the research database.

CI demonstrates the isolated flow: create ephemeral PostgreSQL, apply `supabase/migrations/*.sql`, then run `go test -p 1 ./internal/adapters/postgres ./tests/integration`. The tests use synthetic data; this is persistence verification, not evidence of profitable market behavior.

Avoid `docker compose config`: it can print secrets after dotenv interpolation. The existing `make verify` target and older Lab gate definitions include that command, so use `bash tests/verify-compose-topology.sh` and the explicit README gates instead.

## Existing installations

The public repository, Go module and display name are `solana-market-research` / Solana Market Research. The following runtime identities deliberately remain unchanged:

- Compose project name: `solana-hype-paper-bot`, preserving the runtime-data volume association.
- Supabase `project_id`: `solana-hype-paper-bot`, preserving the local database identity.
- Local Hermes profile and board identifiers, where configured: `solana-hype-paper-bot`.
- Provider user-agent identity: `solana-hype-paper-bot/production-paper`.
- Strategy version, environment-variable names, migration history, and paper-position/report identities.

Renaming these identifiers is a separate data/runtime migration. Do not run `supabase init`, regenerate configuration from a renamed registry slug, reset the database, recreate credentials, or remove volumes as part of a presentation refresh.

Within Coding Lab, the canonical location is `projects/research-projects/solana/market-research`; the registry slug is `solana-market-research`. External shortcuts or scheduled tasks with absolute paths may need an owner-approved path update before the next launch. This refresh does not activate or modify another Hermes profile.
