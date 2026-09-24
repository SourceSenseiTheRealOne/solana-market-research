import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App'

afterEach(() => {
  vi.unstubAllGlobals()
})

function snapshot(overrides: Record<string, unknown> = {}) {
  return {
    strategy_version: 'bold-momentum-v2',
    utc_date: '2026-08-20',
    daily_results: [],
    open_positions: [],
    twitter_analytics: [],
    automation_activity: [],
    reviewed_candidates: [],
    ...overrides,
  }
}

function respond(value: Record<string, unknown>) {
  return new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

describe('App', () => {
  it('shows an explicit read-only loading state before rendering the open positions snapshot', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(snapshot({
      open_positions: [{ id: 7, candidate_id: 9, state: 'OPEN', notional_micros: 10000000, mint_address: 'public-token-mint', no_route_count: 1, opened_at: '2026-08-20T01:00:00Z' }],
    }))))

    render(<App />)

    expect(screen.getByRole('main')).toHaveTextContent('Loading read-only paper-trading data')
    expect(screen.getByRole('heading', { name: 'Paper bot operations' })).toBeVisible()
    expect(screen.getByText('Solana Market Research')).toBeVisible()
    expect(await screen.findByRole('table', { name: 'Open paper positions' })).toHaveTextContent('public-token-mint')
    expect(screen.getByText('Read-only · paper trades only')).toBeVisible()
  })

  it('renders aggregate Twitter search analytics without raw social evidence', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(snapshot({
      twitter_analytics: [{ mint_address: 'twitter-public-mint', searched_at: '2026-08-20T01:00:00Z', search_count: 1, score: 72, posts: 8, unique_authors: 6, exact_mint_mentions: 5, warning_posts: 1 }],
    }))))

    render(<App />)

    const table = await screen.findByRole('table', { name: 'Twitter search analytics' })
    expect(table).toHaveTextContent('twitter-public-mint')
    expect(table).toHaveTextContent('72')
    expect(table).toHaveTextContent('8')
    expect(table).toHaveTextContent('6')
    expect(table).toHaveTextContent('5')
    expect(table).toHaveTextContent('1')
    expect(screen.getByText('No raw tweets or account data are displayed.')).toBeVisible()
  })

  it('renders bounded safe activity and reviewed-token tables', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(snapshot({
      automation_activity: [{ occurred_at: '2026-08-20T02:00:00Z', job: 'SCAN', outcome: 'COMPLETED', category: '', stage: '' }],
      reviewed_candidates: [{ mint_address: 'reviewed-public-mint', checked_at: '2026-08-20T03:00:00Z', outcome: 'NOT_TRADED', reason: 'deterministic_liquidity' }],
    }))))

    render(<App />)

    expect(await screen.findByRole('table', { name: 'Automation activity' })).toHaveTextContent('COMPLETED')
    expect(screen.getByRole('table', { name: 'Reviewed non-traded tokens' })).toHaveTextContent('reviewed-public-mint')
    expect(screen.getByRole('table', { name: 'Reviewed non-traded tokens' })).toHaveTextContent('deterministic_liquidity')
    expect(screen.getByText('No provider payloads, credentials, or social evidence are displayed.')).toBeVisible()
  })

  it('renders a generic alert when the local API fails without exposing response details', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('database password must not leak', { status: 500 })))

    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Dashboard data is unavailable')
    expect(screen.queryByText('database password must not leak')).not.toBeInTheDocument()
  })

  it('shows explicit empty states when the snapshot has no safe projections', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(snapshot())))

    render(<App />)

    expect(await screen.findByText('No open paper positions are in the current snapshot.')).toBeVisible()
    expect(screen.getByText('Active strategy: bold-momentum-v2')).toBeVisible()
    expect(screen.queryByRole('table', { name: 'Open paper positions' })).not.toBeInTheDocument()
    expect(screen.getByText('No completed Twitter searches are in the current snapshot.')).toBeVisible()
    expect(screen.queryByRole('table', { name: 'Twitter search analytics' })).not.toBeInTheDocument()
    expect(screen.getByText('No automation activity is in the current snapshot.')).toBeVisible()
    expect(screen.getByText('No reviewed non-traded tokens are in the current snapshot.')).toBeVisible()
  })
})
