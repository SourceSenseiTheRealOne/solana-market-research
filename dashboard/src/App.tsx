import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { fetchDashboard, type AutomationActivity, type DailyResult, type OpenPosition, type ReviewedCandidate, type TwitterAnalytics } from './api'
import './App.css'

const pollIntervalMilliseconds = 15_000

function App() {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  )

  return <QueryClientProvider client={queryClient}><Dashboard /></QueryClientProvider>
}

function Dashboard() {
  const date = utcDateToday()
  const dashboard = useQuery({
    queryKey: ['dashboard', date],
    queryFn: ({ signal }) => fetchDashboard(date, signal),
    refetchInterval: pollIntervalMilliseconds,
  })

  return (
    <main className="dashboard-shell">
      <header className="page-header">
        <div><p className="eyebrow">Solana Market Research</p><h1>Paper bot operations</h1></div>
        <p className="safety-label">Read-only · paper trades only</p>
      </header>
      {dashboard.isPending ? <LoadingPanel /> : null}
      {dashboard.isError ? <ErrorPanel /> : null}
      {dashboard.data ? <DashboardData {...dashboard.data} /> : null}
    </main>
  )
}

function LoadingPanel() {
  return <section className="state-panel" aria-live="polite" aria-label="Dashboard loading state"><p>Loading read-only paper-trading data…</p></section>
}

function ErrorPanel() {
  return <section className="state-panel state-panel-error" role="alert"><h2>Dashboard data is unavailable</h2><p>The local API did not return a safe snapshot. It will retry automatically.</p></section>
}

function DashboardData({ dailyResults, openPositions, twitterAnalytics, automationActivity, reviewedCandidates, strategyVersion, utcDate }: { dailyResults: DailyResult[]; openPositions: OpenPosition[]; twitterAnalytics: TwitterAnalytics[]; automationActivity: AutomationActivity[]; reviewedCandidates: ReviewedCandidate[]; strategyVersion: string; utcDate: string }) {
  return <>
    <StatusPanel openPositionCount={openPositions.length} strategyVersion={strategyVersion} utcDate={utcDate} />
    <MetricCards dailyResults={dailyResults} />
    <OpenPositionsTable positions={openPositions} />
    <TwitterAnalyticsTable analytics={twitterAnalytics} />
    <AutomationActivityTable activity={automationActivity} />
    <ReviewedCandidatesTable candidates={reviewedCandidates} />
  </>
}

function StatusPanel({ openPositionCount, strategyVersion, utcDate }: { openPositionCount: number; strategyVersion: string; utcDate: string }) {
  return <section className="status-panel" aria-labelledby="status-heading"><div><h2 id="status-heading">Local API status</h2><p><span className="status-dot" aria-hidden="true" />Snapshot available for {utcDate} UTC.</p><p>Active strategy: {strategyVersion}</p></div><p><strong>{openPositionCount}</strong> open paper position{openPositionCount === 1 ? '' : 's'}</p></section>
}

function MetricCards({ dailyResults }: { dailyResults: DailyResult[] }) {
  const admitted = dailyResults.reduce((total, result) => total + BigInt(result.dailyAdmittedCount), 0n)
  const realizedPnl = dailyResults.reduce((total, result) => total + BigInt(result.realizedPnlMicros), 0n)
  return <section aria-labelledby="metrics-heading"><h2 id="metrics-heading" className="section-heading">Daily results</h2><div className="metric-grid">
    <article className="metric-card"><h3>Admissions</h3><p>{admitted.toString()}</p><span>Across persisted strategies</span></article>
    <article className="metric-card"><h3>Realized P&amp;L</h3><p>{formatMicros(realizedPnl)}</p><span>UTC day, paper positions only</span></article>
    <article className="metric-card"><h3>Strategies reported</h3><p>{dailyResults.length}</p><span>Retained daily result rows</span></article>
  </div></section>
}

function OpenPositionsTable({ positions }: { positions: OpenPosition[] }) {
  return <section aria-labelledby="positions-heading"><h2 id="positions-heading" className="section-heading">Open positions</h2>{positions.length === 0 ? <p className="empty-state">No open paper positions are in the current snapshot.</p> : <div className="table-scroll"><table aria-label="Open paper positions"><caption>Open paper positions</caption><thead><tr><th scope="col">Position</th><th scope="col">Candidate</th><th scope="col">Mint</th><th scope="col">Notional</th><th scope="col">Route misses</th><th scope="col">Opened UTC</th></tr></thead><tbody>{positions.map((position) => <tr key={position.id}><td>{position.id}</td><td>{position.candidateId}</td><td className="mint-address">{position.mintAddress}</td><td>{formatMicros(BigInt(position.notionalMicros))}</td><td>{position.noRouteCount}</td><td>{position.openedAt || 'Unavailable'}</td></tr>)}</tbody></table></div>}</section>
}

function TwitterAnalyticsTable({ analytics }: { analytics: TwitterAnalytics[] }) {
  return <section aria-labelledby="twitter-analytics-heading"><h2 id="twitter-analytics-heading" className="section-heading">Twitter search analytics</h2>{analytics.length === 0 ? <p className="empty-state">No completed Twitter searches are in the current snapshot.</p> : <><p className="analytics-notice">No raw tweets or account data are displayed.</p><div className="table-scroll"><table aria-label="Twitter search analytics"><caption>Twitter search analytics</caption><thead><tr><th scope="col">Mint</th><th scope="col">Searched UTC</th><th scope="col">Searches</th><th scope="col">Score</th><th scope="col">Posts</th><th scope="col">Authors</th><th scope="col">Mint mentions</th><th scope="col">Warnings</th></tr></thead><tbody>{analytics.map((item) => <tr key={`${item.mintAddress}:${item.searchedAt}`}><td className="mint-address">{item.mintAddress}</td><td>{item.searchedAt}</td><td>{item.searchCount}</td><td>{item.score}</td><td>{item.posts}</td><td>{item.uniqueAuthors}</td><td>{item.exactMintMentions}</td><td>{item.warningPosts}</td></tr>)}</tbody></table></div></>}</section>
}

function AutomationActivityTable({ activity }: { activity: AutomationActivity[] }) {
  return <section aria-labelledby="automation-activity-heading"><h2 id="automation-activity-heading" className="section-heading">Automation activity</h2>{activity.length === 0 ? <p className="empty-state">No automation activity is in the current snapshot.</p> : <div className="table-scroll"><table aria-label="Automation activity"><caption>Automation activity</caption><thead><tr><th scope="col">Occurred UTC</th><th scope="col">Job</th><th scope="col">Outcome</th><th scope="col">Category</th><th scope="col">Stage</th></tr></thead><tbody>{activity.map((item) => <tr key={`${item.occurredAt}:${item.job}:${item.outcome}`}><td>{item.occurredAt}</td><td>{item.job}</td><td>{item.outcome}</td><td>{item.category || '—'}</td><td>{item.stage || '—'}</td></tr>)}</tbody></table></div>}</section>
}

function ReviewedCandidatesTable({ candidates }: { candidates: ReviewedCandidate[] }) {
  return <section aria-labelledby="reviewed-candidates-heading"><h2 id="reviewed-candidates-heading" className="section-heading">Reviewed non-traded tokens</h2>{candidates.length === 0 ? <p className="empty-state">No reviewed non-traded tokens are in the current snapshot.</p> : <><p className="analytics-notice">No provider payloads, credentials, or social evidence are displayed.</p><div className="table-scroll"><table aria-label="Reviewed non-traded tokens"><caption>Reviewed non-traded tokens</caption><thead><tr><th scope="col">Mint</th><th scope="col">Checked UTC</th><th scope="col">Outcome</th><th scope="col">Reason</th></tr></thead><tbody>{candidates.map((candidate) => <tr key={`${candidate.mintAddress}:${candidate.checkedAt}`}><td className="mint-address">{candidate.mintAddress}</td><td>{candidate.checkedAt}</td><td>{candidate.outcome}</td><td>{candidate.reason}</td></tr>)}</tbody></table></div></>}</section>
}

function formatMicros(value: bigint): string {
  const absolute = value < 0n ? -value : value
  const dollars = absolute / 1_000_000n
  const cents = (absolute % 1_000_000n) / 10_000n
  return `${value < 0n ? '-' : ''}$${dollars.toString()}.${cents.toString().padStart(2, '0')}`
}

function utcDateToday(): string {
  return new Date().toISOString().slice(0, 10)
}

export default App
