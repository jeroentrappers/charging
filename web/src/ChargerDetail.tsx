import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, type Charger, type LiveStatus, type PricePoint, type TariffComponent, type TariffRestrictions } from './api'
import { AvailBadge, availOf, ago, eur } from './ui'

function isIdle(type: string): boolean {
  return type === 'PARKING_TIME'
}
function mins(seconds: number): number {
  return Math.round(seconds / 60)
}
// OCPI prices TIME/PARKING_TIME per hour; drivers think in €/min, so show both.
function valueText(c: TariffComponent, t: TFunction): string {
  switch (c.type) {
    case 'ENERGY': return `${eur(c.price)} ${t('unit.kwh')}`
    case 'FLAT': return `${eur(c.price)} ${t('unit.session')}`
    case 'TIME':
    case 'PARKING_TIME': {
      const perMin = c.price > 0 ? ` (€${(c.price / 60).toFixed(2)}${t('unit.min')})` : ''
      return `${eur(c.price)} ${t('unit.hour')}${perMin}`
    }
    default: return eur(c.price)
  }
}
function restrictionText(r: TariffRestrictions | undefined, t: TFunction): string {
  if (!r) return ''
  const parts: string[] = []
  if (r.start_time || r.end_time) parts.push(`${r.start_time ?? '00:00'}–${r.end_time ?? '24:00'}`)
  if (r.day_of_week?.length) parts.push(r.day_of_week.map((d) => d.slice(0, 3).toLowerCase()).join(', '))
  if (r.min_duration != null) parts.push(t('restr.afterMin', { n: mins(r.min_duration) }))
  if (r.max_duration != null) parts.push(t('restr.uptoMin', { n: mins(r.max_duration) }))
  if (r.min_power != null || r.max_power != null) {
    parts.push(r.min_power != null && r.max_power != null ? t('restr.powerRange', { min: r.min_power, max: r.max_power })
      : r.min_power != null ? t('restr.minPower', { n: r.min_power }) : t('restr.maxPower', { n: r.max_power }))
  }
  if (r.min_kwh != null || r.max_kwh != null) {
    parts.push(r.min_kwh != null && r.max_kwh != null ? t('restr.kwhRange', { min: r.min_kwh, max: r.max_kwh })
      : r.min_kwh != null ? t('restr.fromKwh', { n: r.min_kwh }) : t('restr.uptoKwh', { n: r.max_kwh }))
  }
  return parts.join(' · ')
}

export function ChargerDetail({ charger, onClose }: { charger: Charger; onClose: () => void }) {
  const { t } = useTranslation()
  const [history, setHistory] = useState<PricePoint[] | null>(null)
  const [live, setLive] = useState<LiveStatus | null>(null)
  const [liveLoading, setLiveLoading] = useState(false)

  useEffect(() => {
    let alive = true
    api.priceHistory(charger.id).then((r) => alive && setHistory(r.history)).catch(() => alive && setHistory([]))
    return () => {
      alive = false
    }
  }, [charger.id])

  // On open, ask for the freshest status. Only Monta chargers (with creds
  // configured) come back as a live reading; otherwise the stored badge stands.
  function refreshLive() {
    setLiveLoading(true)
    api.live(charger.id).then(setLive).catch(() => {}).finally(() => setLiveLoading(false))
  }
  useEffect(() => {
    setLive(null)
    refreshLive()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [charger.id])

  const chart = (history ?? [])
    .filter((h) => h.comparable_price_eur != null)
    .slice()
    .reverse()
    .map((h) => ({ t: h.observed_from.slice(0, 10), price: h.comparable_price_eur as number }))

  const matrix = Object.entries(charger.comparable_prices).sort(([a], [b]) => a.localeCompare(b))
  const current = (history ?? []).find((h) => h.observed_to == null) ?? (history ?? [])[0] ?? null
  const elements = current?.price_components?.elements ?? []
  const hasIdle = elements.some((el) => el.price_components.some((c) => isIdle(c.type) && c.price > 0))
  const nav = `https://www.openstreetmap.org/directions?to=${charger.lat},${charger.lon}`

  return (
    <div className="overlay" onClick={onClose}>
      <div className="detail" onClick={(e) => e.stopPropagation()}>
        <div className="detail-head">
          <div>
            <h2>{charger.name || 'Charger'}</h2>
            <div className="muted">{charger.address || `${charger.cpo_id}`}</div>
          </div>
          <button className="btn" onClick={onClose}>{t('detail.close')}</button>
        </div>

        <div style={{ marginTop: 8 }}>
          <AvailBadge a={availOf(charger)} /> <span className="muted">· {t('detail.updated', { ago: ago(charger.status_updated_at, t) })}</span>
        </div>
        {live?.source === 'live' && (
          <div className="live-row" style={{ marginTop: 6 }}>
            <span className={`badge ${live.available ? 'free' : 'busy'}`}>● {t('detail.live')}: {t(live.available ? 'avail.free' : 'avail.busy')}</span>
            <span className="muted"> · {t('detail.updated', { ago: ago(live.checked_at, t) })}</span>
            <button className="btn" onClick={refreshLive} disabled={liveLoading} style={{ marginLeft: 8 }}>
              {liveLoading ? t('detail.checking') : t('detail.refresh')}
            </button>
          </div>
        )}

        <div className="kv">
          <div className="cell"><div className="k">{t('detail.priceThisSession')}</div><div className="v">{eur(charger.session_price_eur ?? charger.comparable_price_eur)}</div></div>
          <div className="cell"><div className="k">{t('detail.power')}</div><div className="v">{charger.power_kw} kW {charger.current_type}</div></div>
          <div className="cell"><div className="k">{t('detail.plug')}</div><div className="v">{charger.plug_type || '—'}</div></div>
          <div className="cell"><div className="k">{t('detail.distance')}</div><div className="v">{Math.round(charger.distance_m)} m</div></div>
        </div>

        {elements.length > 0 && (
          <>
            <h3 style={{ margin: '12px 0 6px' }}>{t('detail.howBuilt')}</h3>
            {elements.map((el, i) => (
              <div key={i} className="tariff-el">
                {el.restrictions && restrictionText(el.restrictions, t) && (
                  <div className="muted tariff-when">{t('detail.when')} {restrictionText(el.restrictions, t)}</div>
                )}
                <table className="matrix">
                  <tbody>
                    {el.price_components.map((c, j) => (
                      <tr key={j} className={isIdle(c.type) && c.price > 0 ? 'warn' : ''}>
                        <td>{t(`comp.${c.type}`, c.type)}</td>
                        <td className="num">{valueText(c, t)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ))}
            {hasIdle && <p className="warn-note">⚠ {t('detail.idleWarning')}</p>}
            <p className="caveat">{t('detail.combineNote')}</p>
          </>
        )}

        <h3 style={{ margin: '6px 0' }}>{t('detail.priceHistory')}</h3>
        {history == null ? (
          <div className="state"><div className="spinner" />{t('insights.loading')}</div>
        ) : chart.length < 2 ? (
          <div className="muted" style={{ fontSize: 13 }}>{t('detail.notEnoughHistory')}</div>
        ) : (
          <ResponsiveContainer width="100%" height={180}>
            <LineChart data={chart} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
              <XAxis dataKey="t" fontSize={11} />
              <YAxis fontSize={11} width={42} domain={['auto', 'auto']} />
              <Tooltip formatter={(v: number) => eur(v)} />
              <Line type="stepAfter" dataKey="price" stroke="#15803d" strokeWidth={2} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        )}

        {matrix.length > 0 && (
          <>
            <h3 style={{ margin: '12px 0 6px' }}>{t('detail.allSessions')}</h3>
            <table className="matrix">
              <tbody>
                {matrix.map(([k, v]) => (
                  <tr key={k}>
                    <td>{t(`session.label.${k}`, k)}</td>
                    <td className="num">{eur(v)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}

        <p className="caveat">{t('detail.caveat')}</p>
        <a className="btn primary" href={nav} target="_blank" rel="noreferrer" style={{ display: 'inline-block', marginTop: 6 }}>
          {t('detail.navigate')}
        </a>
      </div>
    </div>
  )
}
