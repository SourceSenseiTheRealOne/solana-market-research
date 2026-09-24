# Security Context

## Data classification

Public source may contain sanitized configuration names and reviewed migrations. Local-only confidential data includes provider API keys, Hermes credentials, raw provider responses, local Supabase credentials/state, logs, reports, and all session material. Wallet secrets must never exist in this project.

## Trust boundaries

The local browser, Go process, local Supabase/Postgres, Docker host gateway, public market/RPC/social APIs, restricted Hermes profile, CI, and GitHub repository are separate boundaries. Provider credentials remain server-side in ignored `.env.local`; browser code receives none.

## Authentication and authorization

No auth is implemented because the dashboard is read-only and binds only to loopback. There are no mutation endpoints. Direct browser Supabase traffic and Supabase Auth are prohibited.

## Threat controls

Provider adapters enforce allowlisted base URLs, context deadlines, response-size/page bounds, input validation, and redacted errors. TwitterAPI.io social searches are exact-mint and time-window bounded. Birdeye discovery reads at most 20 Solana listings and does not persist raw security-report payloads. Persistent idempotency and transactional admission enforce quota safety. HTTP exposes only allowlisted read DTOs. Structured logs exclude authorization, keys, headers, query strings, request bodies, raw payloads, and provider secrets. Dependency checks reject wallet, signing, swap-execution, and send-transaction packages.

## Secrets and signing material

`.env.local`, `.env`, local Supabase state, Hermes profile state, credentials, and reports are ignored. Rotate provider/Hermes keys outside Git. Never place personal TwitterAPI.io credentials in Hermes configuration or source. No seed phrase, private key, signing API, wallet package, or transaction construction dependency is permitted.

## Approval boundaries

Human approval is required for any provider credential creation, hosted deployment, destructive database reset/volume deletion, remote migration, production resource, paid API usage beyond agreed limits, or any proposal to add blockchain-write capability. Only read-only Solana Mainnet market/RPC evidence is used; blockchain writes and real-fund execution remain outside scope.
