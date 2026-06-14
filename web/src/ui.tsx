import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import type { Charger } from './api'
import { energyPresets, type Settings } from './settings'
import { LANGS } from './i18n'
import type { Theme } from './theme'
import { CARS, carLabel, carPlugs } from './cars'
import { MSPS } from './msps'

export function eur(n: number | null | undefined): string {
  return n == null ? '—' : '€' + n.toFixed(2)
}

export function km(m: number): string {
  return m < 1000 ? `${Math.round(m)} m` : `${(m / 1000).toFixed(1)} km`
}

// Friendly plug-type labels. The raw value differs by source (OCPI
// "IEC_62196_T2" vs DATEX "iec62196T2"), so normalize then map.
const PLUG_LABELS: Record<string, string> = {
  IEC62196T2: 'Type 2', // Mennekes
  IEC62196T2COMBO: 'CCS', // Combo 2
  IEC62196T1: 'Type 1',
  IEC62196T3C: 'Type 3C',
  CHADEMO: 'CHAdeMO',
  TESLAS: 'Tesla',
  DOMESTICF: 'Domestic',
}
export function plugLabel(raw: string): string {
  if (!raw) return '—'
  return PLUG_LABELS[raw.toUpperCase().replace(/[_\s]/g, '')] ?? raw
}

export type Avail = 'free' | 'busy' | 'unknown'

export function availOf(c: Charger): Avail {
  if (c.availability_stale || c.status_updated_at == null) return 'unknown'
  return c.available_count > 0 ? 'free' : 'busy'
}

// Trust level of a price by how we sourced it: OCPI is a direct operator feed;
// everything else is an open-data / aggregator feed.
export function sourceConfidence(sourceType?: string): 'direct' | 'aggregator' {
  return sourceType === 'ocpi' ? 'direct' : 'aggregator'
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

// ProfileBar is the simplified charging profile: how much energy + how fast.
// (The full price comparison reduces to energy × €/kWh + duration × €/h + the
// per-session flat fee — so energy and speed are all the user needs to pick.)
export function ProfileBar(props: {
  car: Settings['car']
  charge: Settings['charge']
  onCharge: (c: Settings['charge']) => void
}) {
  const { t } = useTranslation()
  const presets = energyPresets(props.car)
  const { charge } = props
  // Don't offer charging speeds the car can't reach (its DC max is the ceiling;
  // AC charging is further capped per-charger in pricing).
  const maxKW = props.car.maxDcKw ?? 0
  const speeds = CUSTOM_POWERS.filter((p) => maxKW <= 0 || p <= maxKW + 0.5)
  return (
    <>
      <div className="seg-row">
        <span className="seg-label">{t('profile.energy')}</span>
        <div className="segs">
          {presets.map((p) => (
            <button key={p.key} className={`seg ${charge.kWh === p.kWh ? 'on' : ''}`} onClick={() => props.onCharge({ ...charge, kWh: p.kWh })}>
              {t(`profile.preset.${p.key}`)} <span className="seg-sub">{p.kWh} kWh</span>
            </button>
          ))}
          <label className="seg seg-input">
            <input
              type="number" min={1} max={250} step={1} inputMode="numeric"
              value={charge.kWh}
              onChange={(e) => props.onCharge({ ...charge, kWh: Math.max(1, Math.min(250, Number(e.target.value) || 0)) })}
            />
            <span>kWh</span>
          </label>
        </div>
      </div>
      <div className="seg-row">
        <span className="seg-label">{t('profile.speed')}</span>
        <div className="segs">
          <button className={`seg ${charge.powerKW == null ? 'on' : ''}`} onClick={() => props.onCharge({ ...charge, powerKW: null })}>
            {t('session.fastest')}
          </button>
          {speeds.map((p) => (
            <button key={p} className={`seg ${charge.powerKW === p ? 'on' : ''}`} onClick={() => props.onCharge({ ...charge, powerKW: p })}>
              {t('session.kw', { n: p })}
            </button>
          ))}
        </div>
      </div>
    </>
  )
}

const THEMES: Theme[] = ['light', 'dark', 'system']
const MAKES = [...new Set(CARS.map((c) => c.make))] // distinct makes, in dataset order

// SettingsPanel edits display preferences (language, theme) and the persisted
// car parameters + detour weighting.
export function SettingsPanel(props: {
  settings: Settings
  onChange: (p: Partial<Settings>) => void
  theme: Theme
  onTheme: (t: Theme) => void
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const { car, detour } = props.settings
  const num = (v: string, min: number) => Math.max(min, Number(v) || min)
  return (
    <div className="overlay" onClick={props.onClose}>
      <div className="detail" onClick={(e) => e.stopPropagation()}>
        <div className="detail-head">
          <h2>{t('settings.title')}</h2>
          <button className="btn" onClick={props.onClose}>{t('detail.close')}</button>
        </div>

        <h3 style={{ margin: '12px 0 6px' }}>{t('settings.display')}</h3>
        <label className="setting-row">
          <span>{t('settings.language')}</span>
          <select value={i18n.resolvedLanguage} onChange={(e) => i18n.changeLanguage(e.target.value)}>
            {LANGS.map((l) => (
              <option key={l.code} value={l.code}>{l.label}</option>
            ))}
          </select>
        </label>
        <label className="setting-row">
          <span>{t('settings.theme')}</span>
          <span className="pills theme-seg">
            {THEMES.map((th) => (
              <button key={th} className={props.theme === th ? 'on' : ''} onClick={() => props.onTheme(th)}>
                {t(`theme.${th}`)}
              </button>
            ))}
          </span>
        </label>

        <h3 style={{ margin: '12px 0 6px' }}>{t('settings.car')}</h3>
        <label className="setting-row">
          <span>{t('settings.carModel')}</span>
          <select
            value={car.modelId ?? ''}
            onChange={(e) => {
              const c = CARS.find((x) => x.id === e.target.value)
              if (!c) {
                props.onChange({ car: { ...car, modelId: undefined } })
                return
              }
              const charge = props.settings.charge
              props.onChange({
                car: {
                  usableKWh: c.usableKWh,
                  consumptionKWh100: c.consumptionKWh100,
                  modelId: c.id,
                  plugs: carPlugs(c),
                  maxAcKw: c.acKw,
                  maxDcKw: c.dcKw,
                },
                // Drop a target speed the new car can't reach (fall back to "fastest").
                ...(charge.powerKW != null && charge.powerKW > c.dcKw ? { charge: { ...charge, powerKW: null } } : {}),
              })
            }}
          >
            <option value="">{t('settings.customCar')}</option>
            {MAKES.map((make) => (
              <optgroup key={make} label={make}>
                {CARS.filter((c) => c.make === make).map((c) => (
                  <option key={c.id} value={c.id}>{carLabel(c)}</option>
                ))}
              </optgroup>
            ))}
          </select>
        </label>
        <label className="setting-row">
          <span>{t('settings.battery')}</span>
          <input type="number" min={1} step={1} value={car.usableKWh}
            onChange={(e) => props.onChange({ car: { ...car, usableKWh: num(e.target.value, 1), modelId: undefined } })} />
        </label>
        <label className="setting-row">
          <span>{t('settings.consumption')}</span>
          <input type="number" min={1} step={0.5} value={car.consumptionKWh100}
            onChange={(e) => props.onChange({ car: { ...car, consumptionKWh100: num(e.target.value, 1), modelId: undefined } })} />
        </label>

        <h3 style={{ margin: '12px 0 6px' }}>{t('settings.memberships')}</h3>
        <p className="caveat" style={{ marginTop: 0 }}>{t('settings.membershipsNote')}</p>
        <div className="member-list">
          {MSPS.map((m) => {
            const on = props.settings.memberships.includes(m.id)
            return (
              <label key={m.id} className={`member ${on ? 'on' : ''}`}>
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) =>
                    props.onChange({
                      memberships: e.target.checked
                        ? [...props.settings.memberships, m.id]
                        : props.settings.memberships.filter((x) => x !== m.id),
                    })
                  }
                />
                <span className="member-name">{m.name}</span>
                <span className="member-rate">
                  {m.kind === 'markup' ? (
                    <>
                      {t('settings.cardAdhoc')}
                      {m.markupEurKWh > 0 && <> +{eur(m.markupEurKWh)} {t('unit.kwh')}</>}
                      {m.sessionFee > 0 && <> +{eur(m.sessionFee)}</>}
                    </>
                  ) : (
                    <>~{eur(m.acEurKWh)}/{eur(m.dcEurKWh)} {t('unit.kwh')}</>
                  )}
                </span>
              </label>
            )
          })}
        </div>

        <h3 style={{ margin: '12px 0 6px' }}>{t('settings.detour')}</h3>
        <label className="setting-row">
          <span>{t('settings.detourEnable')}</span>
          <input type="checkbox" checked={detour.enabled}
            onChange={(e) => props.onChange({ detour: { ...detour, enabled: e.target.checked } })} />
        </label>
        {detour.enabled && (
          <>
            <label className="setting-row">
              <span>{t('settings.refPrice')}</span>
              <input type="number" min={0} step={0.01} value={detour.refPrice}
                onChange={(e) => props.onChange({ detour: { ...detour, refPrice: num(e.target.value, 0) } })} />
            </label>
            <label className="setting-row">
              <span>{t('settings.valuePerH')}</span>
              <input type="number" min={0} step={1} value={detour.eurPerHour}
                onChange={(e) => props.onChange({ detour: { ...detour, eurPerHour: num(e.target.value, 0) } })} />
            </label>
          </>
        )}
        <p className="caveat">{t('settings.note')}</p>
      </div>
    </div>
  )
}

export interface Filters {
  available: boolean
  minPower: number
  plug: string
  includePrivate: boolean
  plugCompatible: boolean // only chargers whose plug fits the selected car
}

const PLUGS = [
  { v: '', key: 'filters.anyPlug' },
  { v: 'IEC_62196_T2', key: 'filters.plug.type2' },
  { v: 'IEC_62196_T2_COMBO', key: 'filters.plug.ccs' },
  { v: 'CHADEMO', key: 'filters.plug.chademo' },
]
const POWERS = [0, 22, 50, 150]

export function FilterBar({ f, onChange, carPlugs }: { f: Filters; onChange: (f: Filters) => void; carPlugs?: string[] }) {
  const { t } = useTranslation()
  return (
    <div className="filters">
      <button className={`chip ${f.available ? 'on' : ''}`} onClick={() => onChange({ ...f, available: !f.available })}>
        {f.available ? '✓ ' : ''}{t('filters.available')}
      </button>
      {carPlugs && carPlugs.length > 0 && (
        <button className={`chip ${f.plugCompatible ? 'on' : ''}`} onClick={() => onChange({ ...f, plugCompatible: !f.plugCompatible })}>
          {f.plugCompatible ? '✓ ' : ''}{t('filters.fitsCar')}
        </button>
      )}
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
      <button className={`chip ${f.includePrivate ? 'on' : ''}`} onClick={() => onChange({ ...f, includePrivate: !f.includePrivate })}>
        {f.includePrivate ? '✓ ' : ''}{t('filters.includePrivate')}
      </button>
    </div>
  )
}
