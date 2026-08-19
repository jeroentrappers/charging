import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type SourceHealth, type StatusResponse } from './api'

// Location-only sources (no live status, no price feed) — their missing
// freshness is expected, not a fault, so render it neutral. Mirrors the server's
// old dashboard logic. Keyed by source id where a type is shared: `datex` covers
// both the Spanish register (location-only) and feeds that carry more.
const LOCATION_ONLY_TYPES = new Set(['bnetza'])
const LOCATION_ONLY_IDS = new Set(['es-dgt'])

function mode(s: SourceHealth): 'push' | 'poll' | 'off' {
  if (s.type === 'mobilithek') return 'push'
  return s.enabled ? 'poll' : 'off'
}

// ago renders a compact age + a freshness class. expectedEmpty (e.g. a
// location-only source has no availability) reads as neutral, not stale.
function ago(now: number, iso: string | null, expectedEmpty: boolean, neverLabel: string, nowLabel: string) {
  if (!iso) return { text: expectedEmpty ? '—' : neverLabel, cls: expectedEmpty ? 'na' : 'old' }
  const d = now - new Date(iso).getTime()
  let cls = 'ok'
  if (d > 864e5) cls = 'old'
  else if (d > 36e5) cls = 'stale'
  let text: string
  if (d < 6e4) text = nowLabel
  else if (d < 36e5) text = `${Math.floor(d / 6e4)}m`
  else if (d < 864e5) text = `${Math.floor(d / 36e5)}h`
  else text = `${Math.floor(d / 864e5)}d`
  return { text, cls }
}

// pollHealthy: the last ingest pass finished recently (within 2 days — the
// slowest cadence apart from the monthly registers) and without an error.
function pollHealthy(now: number, s: SourceHealth): boolean {
  return !!s.last_run_at && s.last_run_error === '' && now - new Date(s.last_run_at).getTime() < 2 * 864e5
}

type SortKey = 'source' | 'type' | 'mode' | 'poll' | 'country' | 'chargers' | 'priced' | 'avail' | 'status' | 'price'

// Numeric/time columns open descending (biggest/freshest first); text ascending.
const DESC_FIRST = new Set<SortKey>(['poll', 'chargers', 'priced', 'avail', 'status', 'price'])

function sortValue(s: SourceHealth, k: SortKey): string | number {
  switch (k) {
    case 'source': return (s.name || s.id).toLowerCase()
    case 'type': return s.type
    case 'mode': return mode(s)
    case 'poll': return s.last_run_at ? new Date(s.last_run_at).getTime() : -1
    case 'country': return s.country
    case 'chargers': return s.chargers
    case 'priced': return s.priced
    case 'avail': return s.available
    case 'status': return s.newest_status ? new Date(s.newest_status).getTime() : -1
    case 'price': return s.newest_price ? new Date(s.newest_price).getTime() : -1
  }
}

export function StatusPage() {
  const { t } = useTranslation()
  const [data, setData] = useState<StatusResponse | null>(null)
  const [err, setErr] = useState(false)
  const [q, setQ] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 } | null>(null)

  useEffect(() => {
    let alive = true
    const load = () =>
      api
        .status()
        .then((d) => alive && (setData(d), setErr(false)))
        .catch(() => alive && setErr(true))
    load()
    const h = setInterval(load, 30000) // auto-refresh, matching the old ops page
    return () => {
      alive = false
      clearInterval(h)
    }
  }, [])

  const rows = useMemo(() => {
    if (!data) return []
    let out = data.sources
    const needle = q.trim().toLowerCase()
    if (needle) {
      out = out.filter((s) =>
        [s.name, s.id, s.type, s.country, mode(s)].some((f) => f && f.toLowerCase().includes(needle)),
      )
    }
    if (sort) {
      out = [...out].sort((a, b) => {
        const va = sortValue(a, sort.key)
        const vb = sortValue(b, sort.key)
        if (va < vb) return -sort.dir
        if (va > vb) return sort.dir
        return a.id < b.id ? -1 : 1 // stable tiebreak
      })
    }
    return out
  }, [data, q, sort])

  if (err && !data) return <div className="status-page"><div className="state">{t('status.error')}</div></div>
  if (!data) return <div className="status-page"><div className="state"><div className="spinner" />{t('status.loading')}</div></div>

  const now = Date.now()
  const generated = new Date(data.generated).toLocaleString()

  // Header cell: click cycles column → reversed → back to server order.
  const TH = ({ k, label, num }: { k: SortKey; label: string; num?: boolean }) => {
    const active = sort?.key === k
    const arrow = active ? (sort!.dir === 1 ? ' ▲' : ' ▼') : ''
    const onClick = () => {
      const first = DESC_FIRST.has(k) ? -1 : 1
      if (!active) setSort({ key: k, dir: first as 1 | -1 })
      else if (sort!.dir === first) setSort({ key: k, dir: -first as 1 | -1 })
      else setSort(null)
    }
    return (
      <th className={`sortable${num ? ' num' : ''}${active ? ' sorted' : ''}`} onClick={onClick} role="button" tabIndex={0}
          onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && onClick()}>
        {label}{arrow}
      </th>
    )
  }

  return (
    <div className="status-page">
      <div className="status-inner">
        <h1>{t('status.title')}</h1>
        <p className="status-sub">
          {t('status.summary', { count: data.totals.sources })} · {t('status.generated')} {generated} · {t('status.autoRefresh')}
        </p>
        <div className="status-totals">
          <div><span>{t('status.chargers')}</span><b>{data.totals.chargers.toLocaleString()}</b></div>
          <div><span>{t('status.priced')}</span><b>{data.totals.priced.toLocaleString()}</b></div>
          <div><span>{t('status.available')}</span><b>{data.totals.available.toLocaleString()}</b></div>
        </div>
        <input
          className="status-search"
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t('status.searchPlaceholder')}
          aria-label={t('status.searchPlaceholder')}
        />
        <div className="status-table-wrap">
          <table className="status-table">
            <thead>
              <tr>
                <TH k="source" label={t('status.colSource')} />
                <TH k="type" label={t('status.colType')} />
                <TH k="mode" label={t('status.colMode')} />
                <TH k="poll" label={t('status.colPoll')} />
                <TH k="country" label={t('status.colCountry')} />
                <TH k="chargers" label={t('status.colChargers')} num />
                <TH k="priced" label={t('status.colPriced')} num />
                <TH k="avail" label={t('status.colAvail')} num />
                <TH k="status" label={t('status.colStatus')} />
                <TH k="price" label={t('status.colPrice')} />
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => {
                const m = mode(s)
                const locOnly = LOCATION_ONLY_TYPES.has(s.type) || LOCATION_ONLY_IDS.has(s.id)
                // A source whose polls succeed but which yields zero chargers
                // (e.g. a feed still missing coordinates) is empty by upstream
                // content, not broken — render its freshness neutral.
                const emptyFeed = s.chargers === 0 && pollHealthy(now, s)
                const st = ago(now, s.newest_status, locOnly || emptyFeed, t('status.never'), t('status.justNow'))
                const pr = ago(now, s.newest_price, locOnly || emptyFeed || s.priced === 0, t('status.never'), t('status.justNow'))
                // Push sources have no scheduled pulls; their run log is empty
                // by design, so the poll column reads neutral.
                const run = ago(now, s.last_run_at, m === 'push' || !s.enabled, t('status.never'), t('status.justNow'))
                if (s.last_run_error !== '') run.cls = 'old'
                const pct = s.chargers > 0 ? `${Math.round((100 * s.priced) / s.chargers)}%` : ''
                return (
                  <tr key={s.id}>
                    <td>{s.name || s.id}<br /><span className="muted">{s.id}</span></td>
                    <td>{s.type}</td>
                    <td><span className={`pill pill-${m}`}>{t(`status.mode_${m}`)}</span></td>
                    <td className={`fresh-${run.cls}`} title={s.last_run_error ? `${t('status.pollFailed')}: ${s.last_run_error}` : undefined}>
                      {run.text}{s.last_run_error !== '' && ' ⚠'}
                    </td>
                    <td>{s.country}</td>
                    <td className="num">{s.chargers.toLocaleString()}</td>
                    <td className="num">{s.priced.toLocaleString()}{pct && <span className="muted"> ({pct})</span>}</td>
                    <td className="num">{s.available.toLocaleString()}</td>
                    <td className={`fresh-${st.cls}`}>{st.text}</td>
                    <td className={`fresh-${pr.cls}`}>{pr.text}</td>
                  </tr>
                )
              })}
              {rows.length === 0 && (
                <tr><td colSpan={10} className="muted status-empty">{t('status.noMatches')}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
