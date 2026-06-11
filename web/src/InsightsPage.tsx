import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, type Overview, type TrendPoint, type PriceAgg, type SessionStat } from './api'
import { eur, priceColor } from './ui'

// A color-coded horizontal meter — robust on mobile (no axis-width games) and
// fully responsive. Bar width is relative to the largest value shown; the fill
// color runs green (cheap) → red (dear) across the shown range.
function Meters({ rows }: { rows: { key: string; label: string; value: number; sub?: string }[] }) {
  if (rows.length === 0) return null
  const vals = rows.map((r) => r.value)
  const min = Math.min(...vals)
  const max = Math.max(...vals)
  return (
    <div className="meters">
      {rows.map((r) => (
        <div className="meter-row" key={r.key}>
          <span className="meter-label">
            {r.label}
            {r.sub && <span className="meter-sub"> · {r.sub}</span>}
          </span>
          <span className="meter-track">
            <span
              className="meter-fill"
              style={{ width: `${max > 0 ? (r.value / max) * 100 : 0}%`, background: priceColor(r.value, min, max) }}
            />
          </span>
          <span className="meter-val">{eur(r.value)}</span>
        </div>
      ))}
    </div>
  )
}

export function InsightsPage() {
  const { t } = useTranslation()
  const [ov, setOv] = useState<Overview | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [regions, setRegions] = useState<PriceAgg[]>([])
  const [sessions, setSessions] = useState<SessionStat[]>([])
  const [scope, setScope] = useState<'city' | 'postal'>('city')
  const [order, setOrder] = useState<'cheapest' | 'priciest'>('cheapest')
  const [err, setErr] = useState(false)

  useEffect(() => {
    Promise.all([api.overview(), api.trend(12), api.sessionStats()])
      .then(([o, tr, s]) => {
        setOv(o)
        setTrend(tr.trend)
        setSessions(s.sessions)
      })
      .catch(() => setErr(true))
  }, [])

  // The area breakdown re-fetches when the city/postal scope changes.
  useEffect(() => {
    api
      .regions(scope)
      .then((r) => setRegions(r.regions))
      .catch(() => setErr(true))
  }, [scope])

  // Median is the headline statistic everywhere: a handful of chargers carry
  // absurd idle/time fees (max seen ≈ €350), which wrecks the mean.
  const byType = useMemo(() => Object.fromEntries((ov?.by_current_type ?? []).map((a) => [a.group, a])), [ov])

  const areaRows = useMemo(() => {
    const withMedian = regions.filter((r) => r.median_eur != null)
    const sorted = [...withMedian].sort((a, b) =>
      order === 'cheapest' ? a.median_eur! - b.median_eur! : b.median_eur! - a.median_eur!,
    )
    return sorted.slice(0, 12).map((r) => ({
      key: r.group,
      label: r.group,
      value: r.median_eur!,
      sub: t('insights.perCharger', { n: r.count }),
    }))
  }, [regions, order, t])

  const sessionRows = useMemo(
    () =>
      [...sessions]
        .sort((a, b) => a.avg_eur - b.avg_eur)
        .map((s) => ({
          key: s.session,
          label: t(`session.label.${s.session}`, s.session),
          value: s.avg_eur,
          sub: `${eur(s.min_eur)}–${eur(s.max_eur)}`,
        })),
    [sessions, t],
  )

  // Only months that actually have data — history starts mid-2026.
  const chart = useMemo(
    () => trend.filter((p) => p.avg_eur != null).map((p) => ({ month: p.month.slice(2), price: p.avg_eur })),
    [trend],
  )

  if (err) return <div className="insights"><div className="state">{t('insights.error')}</div></div>
  if (!ov) return <div className="insights"><div className="state"><div className="spinner" />{t('insights.loading')}</div></div>

  const all = byType['all']
  const pct = ov.chargers > 0 ? Math.round((ov.priced_chargers / ov.chargers) * 100) : 0

  return (
    <div className="insights">
      <div className="insights-wrap">
        <p className="insights-intro">{t('insights.intro')}</p>

        <div className="cards">
          <div className="card">
            <h3>{t('insights.chargers')}</h3>
            <div className="big">{ov.priced_chargers.toLocaleString()}</div>
            <div className="muted">{t('insights.ofTotal', { n: ov.chargers.toLocaleString() })} · {t('insights.coverage', { pct })}</div>
          </div>
          <div className="card">
            <h3>{t('insights.medianAll')}</h3>
            <div className="big">{eur(all?.median_eur)}</div>
            <div className="muted">{t('insights.perCharger', { n: (all?.count ?? 0).toLocaleString() })}</div>
          </div>
          <div className="card">
            <h3>{t('insights.acMedian')}</h3>
            <div className="big">{eur(byType['AC']?.median_eur)}</div>
            <div className="muted">{t('insights.perCharger', { n: (byType['AC']?.count ?? 0).toLocaleString() })}</div>
          </div>
          <div className="card">
            <h3>{t('insights.dcMedian')}</h3>
            <div className="big">{eur(byType['DC']?.median_eur)}</div>
            <div className="muted">{t('insights.perCharger', { n: (byType['DC']?.count ?? 0).toLocaleString() })}</div>
          </div>
        </div>

        <div className="section">
          <div className="section-head">
            <h3>{t('insights.areaTitle')}</h3>
            <div className="toggles">
              <div className="pills">
                <button className={scope === 'city' ? 'on' : ''} onClick={() => setScope('city')}>{t('insights.byCity')}</button>
                <button className={scope === 'postal' ? 'on' : ''} onClick={() => setScope('postal')}>{t('insights.byPostal')}</button>
              </div>
              <div className="pills">
                <button className={order === 'cheapest' ? 'on' : ''} onClick={() => setOrder('cheapest')}>{t('insights.cheapest')}</button>
                <button className={order === 'priciest' ? 'on' : ''} onClick={() => setOrder('priciest')}>{t('insights.priciest')}</button>
              </div>
            </div>
          </div>
          <Meters rows={areaRows} />
        </div>

        <div className="section">
          <h3>{t('insights.sessionTitle')}</h3>
          <Meters rows={sessionRows} />
        </div>

        <div className="section">
          <h3>{t('insights.trendTitle')}</h3>
          {chart.length >= 2 ? (
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={chart} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                <XAxis dataKey="month" fontSize={11} />
                <YAxis fontSize={11} width={42} domain={['auto', 'auto']} />
                <Tooltip formatter={(v: number) => eur(v)} />
                <Line type="monotone" dataKey="price" stroke="#15803d" strokeWidth={2} dot={false} connectNulls />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <div className="muted" style={{ fontSize: 13 }}>{t('insights.trendNote')}</div>
          )}
        </div>
      </div>
    </div>
  )
}
