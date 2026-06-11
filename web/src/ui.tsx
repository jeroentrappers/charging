import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import type { Charger } from './api'

export function eur(n: number | null | undefined): string {
  return n == null ? '—' : '€' + n.toFixed(2)
}

export function km(m: number): string {
  return m < 1000 ? `${Math.round(m)} m` : `${(m / 1000).toFixed(1)} km`
}

export type Avail = 'free' | 'busy' | 'unknown'

export function availOf(c: Charger): Avail {
  if (c.availability_stale || c.status_updated_at == null) return 'unknown'
  return c.available_count > 0 ? 'free' : 'busy'
}

export function ago(iso: string | null, t: TFunction): string {
  if (!iso) return t('time.unknown')
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 90) return t('time.justNow')
  if (s < 3600) return t('time.minAgo', { n: Math.round(s / 60) })
  if (s < 86400) return t('time.hAgo', { n: Math.round(s / 3600) })
  return t('time.dAgo', { n: Math.round(s / 86400) })
}

// price -> green(cheap)..red(pricey) on a hue scale, clamped.
export function priceColor(p: number | null, min: number, max: number): string {
  if (p == null) return '#94a3b8'
  const t = max > min ? (p - min) / (max - min) : 0
  const hue = 140 - 140 * Math.min(1, Math.max(0, t))
  return `hsl(${hue} 70% 42%)`
}

export function priceOf(c: Charger): number | null {
  return c.session_price_eur ?? c.comparable_price_eur
}

export function AvailBadge({ a }: { a: Avail }) {
  const { t } = useTranslation()
  return <span className={`badge ${a === 'unknown' ? 'unknown' : a}`}>{t(`avail.${a}`)}</span>
}

// ---- session selector (the hero control) ----

export const NEEDS = ['best', 'topup100', 'charge1080', 'custom'] as const
export const SPEEDS = ['ac11', 'ac22', 'dc150', 'dc300'] as const
export const CUSTOM_POWERS = [11, 22, 50, 150, 300] as const
export const KWH_PRESETS = [20, 50, 75] as const

export function sessionKey(need: string, speed: string): string | undefined {
  return need === 'best' || need === 'custom' ? undefined : `${need}_${speed}`
}

// CustomSession is a user-defined session: energy to add to the battery, plus a
// power cap (null = "as fast as the charger allows").
export interface CustomSession {
  kWh: number
  powerKW: number | null
}

export function SessionBar(props: {
  need: string
  speed: string
  onNeed: (n: string) => void
  onSpeed: (s: string) => void
  custom: CustomSession
  onCustom: (c: CustomSession) => void
}) {
  const { t } = useTranslation()
  const { custom, onCustom } = props
  return (
    <>
      <div className="seg-row">
        <span className="seg-label">{t('session.charge')}</span>
        <div className="segs">
          {NEEDS.map((id) => (
            <button key={id} className={`seg ${props.need === id ? 'on' : ''}`} onClick={() => props.onNeed(id)}>
              {t(`session.need.${id}`)}
            </button>
          ))}
        </div>
      </div>
      {(props.need === 'topup100' || props.need === 'charge1080') && (
        <div className="seg-row">
          <span className="seg-label">{t('session.speed')}</span>
          <div className="segs">
            {SPEEDS.map((id) => (
              <button key={id} className={`seg ${props.speed === id ? 'on' : ''}`} onClick={() => props.onSpeed(id)}>
                {t(`session.speedOpt.${id}`)}
              </button>
            ))}
          </div>
        </div>
      )}
      {props.need === 'custom' && (
        <>
          <div className="seg-row">
            <span className="seg-label">{t('session.energy')}</span>
            <div className="segs">
              {KWH_PRESETS.map((v) => (
                <button key={v} className={`seg ${custom.kWh === v ? 'on' : ''}`} onClick={() => onCustom({ ...custom, kWh: v })}>
                  {t('session.kwh', { n: v })}
                </button>
              ))}
              <label className="seg seg-input">
                <input
                  type="number" min={1} max={250} step={5} inputMode="numeric"
                  value={custom.kWh}
                  onChange={(e) => onCustom({ ...custom, kWh: Math.max(1, Math.min(250, Number(e.target.value) || 0)) })}
                />
                <span>{t('session.kwhUnit')}</span>
              </label>
            </div>
          </div>
          <div className="seg-row">
            <span className="seg-label">{t('session.power')}</span>
            <div className="segs">
              <button className={`seg ${custom.powerKW == null ? 'on' : ''}`} onClick={() => onCustom({ ...custom, powerKW: null })}>
                {t('session.fastest')}
              </button>
              {CUSTOM_POWERS.map((p) => (
                <button key={p} className={`seg ${custom.powerKW === p ? 'on' : ''}`} onClick={() => onCustom({ ...custom, powerKW: p })}>
                  {t('session.kw', { n: p })}
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </>
  )
}

// Report types shown in the charger detail, ordered for the "report an issue"
// UI. `value` indicates which typed input to collect. Labels come from i18n
// (report.type.<key>). Mirrors the backend registry in internal/report.
export const REPORT_TYPES: { key: string; group: string; value?: 'time' | 'kw' | 'price' }[] = [
  { key: 'out_of_service', group: 'status' },
  { key: 'back_in_service', group: 'status' },
  { key: 'spot_blocked', group: 'status' },
  { key: 'not_public', group: 'access' },
  { key: 'access_blocked', group: 'access' },
  { key: 'site_hours', group: 'access', value: 'time' },
  { key: 'slower_than_rated', group: 'performance', value: 'kw' },
  { key: 'price_incorrect', group: 'pricing', value: 'price' },
  { key: 'wrong_location', group: 'data' },
  { key: 'does_not_exist', group: 'data' },
  { key: 'confirmed_ok', group: 'positive' },
]

export interface Filters {
  available: boolean
  minPower: number
  plug: string
}

const PLUGS = [
  { v: '', key: 'filters.anyPlug' },
  { v: 'IEC_62196_T2', key: 'filters.plug.type2' },
  { v: 'IEC_62196_T2_COMBO', key: 'filters.plug.ccs' },
  { v: 'CHADEMO', key: 'filters.plug.chademo' },
]
const POWERS = [0, 22, 50, 150]

export function FilterBar({ f, onChange }: { f: Filters; onChange: (f: Filters) => void }) {
  const { t } = useTranslation()
  return (
    <div className="filters">
      <button className={`chip ${f.available ? 'on' : ''}`} onClick={() => onChange({ ...f, available: !f.available })}>
        {f.available ? '✓ ' : ''}{t('filters.available')}
      </button>
      <label className={`chip ${f.minPower ? 'on' : ''}`}>
        <select value={f.minPower} onChange={(e) => onChange({ ...f, minPower: Number(e.target.value) })}>
          {POWERS.map((v) => (
            <option key={v} value={v}>{v === 0 ? t('filters.anyPower') : t('filters.minPower', { kw: v })}</option>
          ))}
        </select>
      </label>
      <label className={`chip ${f.plug ? 'on' : ''}`}>
        <select value={f.plug} onChange={(e) => onChange({ ...f, plug: e.target.value })}>
          {PLUGS.map((p) => (
            <option key={p.v} value={p.v}>{t(p.key)}</option>
          ))}
        </select>
      </label>
    </div>
  )
}
