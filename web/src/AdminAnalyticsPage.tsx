import { useEffect, useState } from 'react'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { fetchAnalytics, type AnalyticsSummary } from './api'

const TOKEN_KEY = 'charging.admin'
const WINDOWS = [1, 7, 30, 90]

// AdminAnalyticsPage renders the first-party analytics rollup. Admin-only: it
// prompts for the ADMIN_TOKEN (kept in localStorage) and calls /admin/analytics
// with it. Not linked from the nav — reached directly at /admin.
export function AdminAnalyticsPage() {
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) || '')
  const [input, setInput] = useState('')
  const [days, setDays] = useState(7)
  const [data, setData] = useState<AnalyticsSummary | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!token) return
    setLoading(true)
    setErr(null)
    fetchAnalytics(token, days)
      .then((d) => setData(d))
      .catch((e: Error) => {
        if (String(e.message).includes('401')) {
          localStorage.removeItem(TOKEN_KEY)
          setToken('')
          setErr('Invalid or expired admin token.')
        } else {
          setErr('Could not load analytics.')
        }
      })
      .finally(() => setLoading(false))
  }, [token, days])

  function saveToken(e: React.FormEvent) {
    e.preventDefault()
    const t = input.trim()
    if (!t) return
    localStorage.setItem(TOKEN_KEY, t)
    setToken(t)
    setInput('')
  }

  if (!token) {
    return (
      <div className="admin-page">
        <h1>Analytics</h1>
        <form onSubmit={saveToken} className="admin-login">
          <label>Admin token</label>
          <input type="password" value={input} onChange={(e) => setInput(e.target.value)} placeholder="ADMIN_TOKEN" autoFocus />
          <button type="submit">Open</button>
        </form>
        {err && <p className="warn-note">{err}</p>}
      </div>
    )
  }

  const chart = (data?.events_per_day ?? []).map((d) => ({ day: d.day.slice(5), count: d.count }))

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h1>Analytics</h1>
        <div className="admin-controls">
          {WINDOWS.map((w) => (
            <button key={w} className={w === days ? 'active' : ''} onClick={() => setDays(w)}>{w}d</button>
          ))}
          <button
            className="link"
            onClick={() => {
              localStorage.removeItem(TOKEN_KEY)
              setToken('')
            }}
          >
            sign out
          </button>
        </div>
      </div>

      {err && <p className="warn-note">{err}</p>}
      {loading && !data && <div className="state"><div className="spinner" />loading…</div>}

      {data && (
        <>
          <div className="stat-tiles">
            <Tile label="Events" value={data.events} />
            <Tile label="Unique visitors" value={data.unique_visitors} />
            <Tile label="Feed consumers" value={data.feed_consumers} />
          </div>

          {chart.length > 0 && (
            <div className="admin-chart">
              <ResponsiveContainer width="100%" height={200}>
                <LineChart data={chart} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                  <XAxis dataKey="day" fontSize={11} />
                  <YAxis fontSize={11} allowDecimals={false} />
                  <Tooltip />
                  <Line type="monotone" dataKey="count" stroke="#15803d" strokeWidth={2} dot={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}

          <div className="admin-tables">
            <CountTable title="Top events" rows={data.top_events} />
            <CountTable title="Top endpoints" rows={data.top_endpoints} />
            <CountTable title="Feed downloads by format" rows={data.downloads_by_format} />
          </div>
        </>
      )}
    </div>
  )
}

function Tile({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat-tile">
      <div className="stat-value">{value.toLocaleString()}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

function CountTable({ title, rows }: { title: string; rows: { key: string; count: number }[] }) {
  return (
    <div className="admin-table">
      <h3>{title}</h3>
      {rows.length === 0 ? (
        <p className="muted">no data</p>
      ) : (
        <table className="matrix">
          <tbody>
            {rows.map((r) => (
              <tr key={r.key}>
                <td>{r.key}</td>
                <td className="num">{r.count.toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
