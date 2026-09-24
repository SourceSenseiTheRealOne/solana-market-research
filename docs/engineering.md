# Engineering notes

Solana Market Research evaluates a small, versioned paper-trading experiment against public market data. The implementation favors bounded work and inspectable state over throughput. It runs as one Go process with a separate local dashboard and PostgreSQL database.

## From discovery to a virtual position

The [automation composition root](../cmd/paper-bot/automation.go) wires public HTTP adapters into application interfaces. Market sources include DexScreener and GeckoTerminal; Helius supplies Solana Mainnet RPC reads, Jupiter supplies quotes, and TwitterAPI.io supplies exact-mint social evidence.

The [scan job](../internal/application/production_scan_job.go) evaluates a bounded candidate set. It first checks market/token/route eligibility. Eligible candidates then need acceptable social evidence and a validated Hermes verdict. Only after those gates does the job request a quote, reserve a paper admission, and ask the broker to simulate an entry. A favorable model response alone cannot create a position.

The [paper broker](../internal/application/paper_broker.go) and [position manager](../internal/application/position_manager.go) operate through local persistence and read-only quotes. No order is submitted. Separate pending, open, and closing states let the application represent incomplete work rather than inventing a successful fill.

## Atomic admission rather than separate quota checks

Checking a limit and inserting a position in separate transactions would let concurrent evaluations exceed the limit. The [admission repository](../internal/adapters/postgres/admission_repository.go) uses a serializable transaction that:

1. Creates or loads the UTC-day counter for the strategy and conditionally increments it.
2. Checks the admission idempotency key and counts pending, open, and closing positions.
3. Persists the decision, pending position, and position event.
4. Commits all changes together; any earlier rejection rolls the counter reservation back.

A uniqueness conflict becomes a non-admission. Serialization retry is bounded; the automation configuration currently selects one attempt, so contention can reject work rather than retry indefinitely. The [repository tests](../internal/adapters/postgres/admission_repository_test.go) cover duplicate admission, concurrent admission caps, and rejection without a partial decision or position.

## Serial scheduling and durable retries

[SplitCadence](../internal/application/split_cadence.go) runs scans, monitoring, and candidate retries serially. It avoids overlapping work and competing position transitions. The trade-off is explicit: a slow scan can delay a monitor tick. The configured five-minute scan interval and 30-second monitor interval are scheduling targets, not hard real-time guarantees.

A candidate with temporarily incomplete market evidence can enter a [durable retry queue](../internal/adapters/postgres/market_retry_repository.go). The application retains at most five, retries at most one per monitor cycle, and expires retries within ten minutes or the remaining eligible pool age. A lease reserves due work. Every retry re-enters the policy; missing evidence does not become implicit approval. Future-dated pools have a separate bounded watch path.

HTTP clients have deadlines, response-size limits, and bounded attempts. This controls provider load and limits how long a failed upstream can occupy a cycle. It does not make an unavailable provider available or guarantee discovery completeness.

## Advisory model boundary

The local Hermes gateway receives sanitized social evidence through a restricted adapter. The composition root pins provider `openai-codex` and model `gpt-5.6-sol`; operators must provision a compatible restricted gateway separately. The model can produce advisory fields, not call a wallet or broker. Application policy checks verdict confidence and risk fields before admission.

The gateway is local, but its configured model provider can be remote. Local operation does not mean all inference stays on the machine. Provider credentials and raw payloads must not be included in model requests. Prompt/version metadata and a digest support inspection of how a verdict was obtained; they do not establish that the verdict was accurate.

## Persistence and representation

Ent provides typed queries and schema definitions. Reviewed SQL under [`supabase/migrations`](../supabase/migrations) is the migration history; runtime startup does not silently create or upgrade the schema. Production research state stays in the project-owned local Supabase stack. CI uses its own ephemeral PostgreSQL service.

Financial storage uses minor-unit integers or decimal representations rather than `float64`. UTC dates define quota buckets and daily results. Strategy identifiers remain attached to positions and reports so a naming refresh cannot relabel an older experiment as a newer cohort.

## Dashboard boundary

The [HTTP handler](../internal/transport/httpapi/httpapi.go) accepts GET requests on `/healthz` and `/api/v1/dashboard`; other methods receive a method-not-allowed response. The dashboard consumes bounded projections, not database credentials or raw social posts. It has explicit loading, empty, and unavailable states.

The browser talks to the Go API through the dashboard's local proxy. It does not query Supabase or public providers directly. Loopback-only port publishing is the reason this single-user interface does not have authentication. Read-only is not a substitute for authentication if the interface is exposed to a network.

## Policy and trade-offs

| Default | Value |
| --- | --- |
| Strategy | `bold-momentum-v3` |
| Virtual notional | $100 |
| Maximum active positions / new UTC-day admissions | 3 / 30 |
| Maximum pool age / minimum liquidity | 90 minutes / $5,000 |
| Five-minute activity / buy share | 20 transactions / 60% |
| Five-minute turnover / price momentum | 15% volume-to-liquidity / +2% to +60% |
| Maximum quoted entry impact | 10% |
| Take profit / stop loss / maximum hold | +50% / -20% / 45 minutes |

These parameters are an experiment definition, not recommendations. Quotes can change before a hypothetical trade would land. Fees, liquidity gaps, provider omissions, stale evidence, and selection bias can separate paper results from executable outcomes. A simulation does not prove profitability or protect a token from manipulation.

## What the tests establish

Go unit and race tests exercise orchestration and boundaries. PostgreSQL-backed contracts exercise migrations, transaction behavior, idempotency, and persistence with synthetic test inputs. Vitest checks dashboard rendering and response validation. Static guards reject prohibited live-trading dependencies and unsafe topology changes.

Database contracts skip without `TEST_DATABASE_URL`; CI applies every migration to a fresh PostgreSQL instance before running them. Test doubles belong to tests, and their results are not historical market-performance evidence. This documentation refresh does not claim a new supervised live-data run, production readiness, or an audit.

The [architecture diagram](assets/architecture.svg) is a source-level explanation. An [offline HTML version](assets/architecture.html) is also included. Neither depicts a measured deployment or a trading result.
