import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, type Overview, type TrendPoint, type PriceAgg, type SessionStat } from './api'
import { eur } from './ui'

export function InsightsPage() {
  const { t } = useTranslation()
  const [ov, setOv] = useState<Overview | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [regions, setRegions] = useState<PriceAgg[]>([])
  const [sessions, setSessions] = useState<SessionStat[]>([])
  const [err, setErr] = useState(false)

  useEffect(() => {
    Promise.all([api.overview(), api.trend(12), api.regions('city'), api.sessionStats()])
      .then(([o, t, r, s]) => {
        setOv(o)
        setTrend(t.trend)
        setRegions(r.regions)
        setSessions(s.sessions)
      })
      .catch(() => setErr(true))
  }, [])

  if (err) return <div className="insights"><div className="state">{t('insights.error')}</div></div>
  if (!ov) return <div className="insights"><div className="state"><div className="spinner" />{t('insights.loading')}</div></div>

  const byType = Object.fromEntries(ov.by_current_type.map((a) => [a.group, a]))
  const chart = trend.map((p) => ({ month: p.month.slice(2), price: p.avg_eur }))

  return (
    <div className="insights">
      <div className="insights-wrap">
        <div className="cards">
          <div className="card"><h3>{t('insights.chargers')}</h3><div className="big">{ov.chargers}</div><div className="muted">{t('insights.withPrice', { n: ov.priced_chargers })}</div></div>
          <div className="card"><h3>{t('insights.avgAc')}</h3><div className="big">{eur(byType['AC']?.avg_eur)}</div></div>
          <div className="card"><h3>{t('insights.avgDc')}</h3><div className="big">{eur(byType['DC']?.avg_eur)}</div></div>
          <div className="card"><h3>{t('insights.median')}</h3><div className="big">{eur(byType['all']?.median_eur)}</div></div>
        </div>

        <div className="section">
          <h3>{t('insights.trendTitle')}</h3>
          {chart.some((c) => c.price != null) ? (
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={chart} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                <XAxis dataKey="month" fontSize={11} />
                <YAxis fontSize={11} width={42} domain={['auto', 'auto']} />
                <Tooltip formatter={(v: number) => eur(v)} />
                <Line type="monotone" dataKey="price" stroke="#15803d" strokeWidth={2} dot={false} connectNulls />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <div className="muted" style={{ fontSize: 13 }}>{t('insights.noHistory')}</div>
          )}
        </div>

        <div className="section">
          <h3>{t('insights.regionsTitle')}</h3>
          <table className="matrix">
            <thead><tr><th>{t('insights.region')}</th><th className="num">{t('insights.avg')}</th><th className="num">{t('insights.count')}</th></tr></thead>
            <tbody>
              {regions.slice(0, 12).map((r) => (
                <tr key={r.group}>
                  <td>{r.group}</td>
                  <td className="num">{eur(r.avg_eur)}</td>
                  <td className="num">{r.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="section">
          <h3>{t('insights.bySession')}</h3>
          <table className="matrix">
            <thead><tr><th>{t('insights.session')}</th><th className="num">{t('insights.avg')}</th><th className="num">{t('insights.min')}</th><th className="num">{t('insights.max')}</th></tr></thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.session}>
                  <td>{t(`session.label.${s.session}`, s.session)}</td>
                  <td className="num">{eur(s.avg_eur)}</td>
                  <td className="num">{eur(s.min_eur)}</td>
                  <td className="num">{eur(s.max_eur)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
