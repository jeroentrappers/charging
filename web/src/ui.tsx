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

export function ago(iso: string | null): string {
  if (!iso) return 'unknown'
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

// price -> green(cheap)..red(pricey) on a hue scale, clamped.
export function priceColor(p: number | null, min: number, max: number): string {
  if (p == null) return '#94a3b8'
  const t = max > min ? (p - min) / (max - min) : 0
  const hue = 140 - 140 * Math.min(1, Math.max(0, t)) // 140=green -> 0=red
  return `hsl(${hue} 70% 42%)`
}

export function priceOf(c: Charger): number | null {
  return c.session_price_eur ?? c.comparable_price_eur
}

export function AvailBadge({ a }: { a: Avail }) {
  const label = a === 'free' ? 'Free now' : a === 'busy' ? 'In use' : 'Unknown'
  return <span className={`badge ${a === 'unknown' ? 'unknown' : a}`}>{label}</span>
}

// ---- session selector (the hero control) ----

export const NEEDS = [
  { id: 'best', label: 'Best price' },
  { id: 'topup100', label: '100 km top-up' },
  { id: 'charge1080', label: '10→80%' },
] as const

export const SPEEDS = [
  { id: 'ac11', label: 'AC 11' },
  { id: 'ac22', label: 'AC 22' },
  { id: 'dc150', label: 'DC 150' },
  { id: 'dc300', label: 'DC 300' },
] as const

export function sessionKey(need: string, speed: string): string | undefined {
  return need === 'best' ? undefined : `${need}_${speed}`
}

export function SessionBar(props: {
  need: string
  speed: string
  onNeed: (n: string) => void
  onSpeed: (s: string) => void
}) {
  return (
    <>
      <div className="seg-row">
        <span className="seg-label">Charge</span>
        <div className="segs">
          {NEEDS.map((n) => (
            <button key={n.id} className={`seg ${props.need === n.id ? 'on' : ''}`} onClick={() => props.onNeed(n.id)}>
              {n.label}
            </button>
          ))}
        </div>
      </div>
      {props.need !== 'best' && (
        <div className="seg-row">
          <span className="seg-label">Speed</span>
          <div className="segs">
            {SPEEDS.map((s) => (
              <button key={s.id} className={`seg ${props.speed === s.id ? 'on' : ''}`} onClick={() => props.onSpeed(s.id)}>
                {s.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </>
  )
}

export interface Filters {
  available: boolean
  minPower: number
  plug: string
}

const PLUGS = [
  { v: '', label: 'Any plug' },
  { v: 'IEC_62196_T2', label: 'Type 2' },
  { v: 'IEC_62196_T2_COMBO', label: 'CCS' },
  { v: 'CHADEMO', label: 'CHAdeMO' },
]
const POWERS = [
  { v: 0, label: 'Any power' },
  { v: 22, label: '≥ 22 kW' },
  { v: 50, label: '≥ 50 kW' },
  { v: 150, label: '≥ 150 kW' },
]

export function FilterBar({ f, onChange }: { f: Filters; onChange: (f: Filters) => void }) {
  return (
    <div className="filters">
      <button className={`chip ${f.available ? 'on' : ''}`} onClick={() => onChange({ ...f, available: !f.available })}>
        {f.available ? '✓ ' : ''}Available now
      </button>
      <label className={`chip ${f.minPower ? 'on' : ''}`}>
        <select value={f.minPower} onChange={(e) => onChange({ ...f, minPower: Number(e.target.value) })}>
          {POWERS.map((p) => (
            <option key={p.v} value={p.v}>{p.label}</option>
          ))}
        </select>
      </label>
      <label className={`chip ${f.plug ? 'on' : ''}`}>
        <select value={f.plug} onChange={(e) => onChange({ ...f, plug: e.target.value })}>
          {PLUGS.map((p) => (
            <option key={p.v} value={p.v}>{p.label}</option>
          ))}
        </select>
      </label>
    </div>
  )
}
