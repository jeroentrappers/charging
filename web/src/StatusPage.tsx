import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type SourceHealth, type StatusResponse } from './api'

// Location-only registers (no live status, no price feed) — their missing
// freshness is expected, not a fault, so render it neutral. Mirrors the server's
// old dashboard logic.
const LOCATION_ONLY = new Set(['bnetza', 'irve'])

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

export function StatusPage() {
  const { t } = useTranslation()
  const [data, setData] = useState<StatusResponse | null>(null)
  const [err, setErr] = useState(false)

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

  if (err && !data) return <div className="status-page"><div className="state">{t('status.error')}</div></div>
  if (!data) return <div className="status-page"><div className="state"><div className="spinner" />{t('status.loading')}</div></div>

  const now = Date.now()
  const generated = new Date(data.generated).toLocaleString()

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
        <div className="status-table-wrap">
          <table className="status-table">
            <thead>
              <tr>
                <th>{t('status.colSource')}</th>
                <th>{t('status.colType')}</th>
                <th>{t('status.colMode')}</th>
                <th>{t('status.colCountry')}</th>
                <th className="num">{t('status.colChargers')}</th>
                <th className="num">{t('status.colPriced')}</th>
                <th className="num">{t('status.colAvail')}</th>
                <th>{t('status.colStatus')}</th>
                <th>{t('status.colPrice')}</th>
              </tr>
            </thead>
            <tbody>
              {data.sources.map((s) => {
                const m = mode(s)
                const locOnly = LOCATION_ONLY.has(s.type)
                const st = ago(now, s.newest_status, locOnly, t('status.never'), t('status.justNow'))
                const pr = ago(now, s.newest_price, locOnly || s.priced === 0, t('status.never'), t('status.justNow'))
                const pct = s.chargers > 0 ? `${Math.round((100 * s.priced) / s.chargers)}%` : ''
                return (
                  <tr key={s.id}>
                    <td>{s.name || s.id}<br /><span className="muted">{s.id}</span></td>
                    <td>{s.type}</td>
                    <td><span className={`pill pill-${m}`}>{t(`status.mode_${m}`)}</span></td>
                    <td>{s.country}</td>
                    <td className="num">{s.chargers.toLocaleString()}</td>
                    <td className="num">{s.priced.toLocaleString()}{pct && <span className="muted"> ({pct})</span>}</td>
                    <td className="num">{s.available.toLocaleString()}</td>
                    <td className={`fresh-${st.cls}`}>{st.text}</td>
                    <td className={`fresh-${pr.cls}`}>{pr.text}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
