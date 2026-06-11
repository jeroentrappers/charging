// Typed client for the charging HTTP API (read endpoints only; admin is the
// CLI's job). Base URL comes from VITE_API_BASE.

// API base resolution order:
//   1. window.__CONFIG__.apiBase — injected at container startup (/config.js,
//      generated from VITE_API_BASE via envsubst). This is the production path.
//   2. import.meta.env.VITE_API_BASE — build-time value, handy in `pnpm dev`.
//   3. localhost default.
const runtimeBase = typeof window !== 'undefined' ? window.__CONFIG__?.apiBase : ''
const BASE = (runtimeBase || import.meta.env.VITE_API_BASE || 'http://localhost:8080').replace(/\/$/, '')

// Public API origin (e.g. https://charging.appmire.be/api) — used for outbound
// links like the interactive docs.
export const API_BASE = BASE

export interface Charger {
  id: number
  cpo_id: string
  name: string
  address: string
  lat: number
  lon: number
  power_kw: number
  plug_type: string
  current_type: string // AC | DC
  distance_m: number
  available_count: number
  comparable_price_eur: number | null
  session_price_eur?: number | null
  comparable_prices: Record<string, number>
  currency: string
  status_updated_at: string | null
  availability_stale: boolean
  reports?: ReportAgg[] // active community reports
  avoid?: boolean // de-prioritised by corroborated flag reports
}

// One report type's value payload (only some types carry one).
export interface ReportValue {
  close?: string
  open?: string
  kw?: number
  price?: number
}

// Aggregated active community report for a charger.
export interface ReportAgg {
  type: string
  group: string
  count: number
  last_at: string
  value?: ReportValue
  flags: boolean
}

export interface SessionProfile {
  key: string
  label: string
  current: string // AC | DC
  tier_kw: number
  avg_kw: number
  metered_kwh: number
}

export interface TariffComponent {
  type: string // ENERGY | FLAT | TIME | PARKING_TIME
  price: number
  step_size: number
}
export interface TariffRestrictions {
  start_time?: string
  end_time?: string
  start_date?: string
  end_date?: string
  min_kwh?: number
  max_kwh?: number
  min_power?: number
  max_power?: number
  min_duration?: number
  max_duration?: number
  day_of_week?: string[]
}
export interface TariffElement {
  price_components: TariffComponent[]
  restrictions?: TariffRestrictions
}
export interface TariffStruct {
  ocpi_id?: string
  currency: string
  elements: TariffElement[]
}

export interface PricePoint {
  comparable_price_eur: number | null
  comparable_prices: Record<string, number>
  price_components: TariffStruct | null
  currency: string
  observed_from: string
  observed_to: string | null
  source_last_updated: string | null
}

export interface PriceAgg {
  group: string
  count: number
  avg_eur: number | null
  median_eur: number | null
  min_eur: number | null
  max_eur: number | null
}

export interface Overview {
  chargers: number
  priced_chargers: number
  by_current_type: PriceAgg[]
}

export interface TrendPoint {
  month: string
  avg_eur: number | null
  count: number
}

export interface SessionStat {
  session: string
  count: number
  avg_eur: number
  min_eur: number
  max_eur: number
}

async function get<T>(path: string, params?: Record<string, string | number | boolean | undefined>): Promise<T> {
  const url = new URL(BASE + path)
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '' && v !== false) url.searchParams.set(k, String(v))
    }
  }
  const res = await fetch(url.toString())
  if (!res.ok) throw new Error(`API ${res.status}`)
  return res.json() as Promise<T>
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`API ${res.status}`)
  return res.json() as Promise<T>
}

// Stable anonymous client id (localStorage) so a user's reports dedupe across
// IP changes without any login.
function clientId(): string {
  try {
    let id = localStorage.getItem('charging.cid')
    if (!id) {
      id = (crypto.randomUUID?.() ?? String(Math.random()).slice(2))
      localStorage.setItem('charging.cid', id)
    }
    return id
  } catch {
    return ''
  }
}

export interface ReportsResult {
  charger_id: number
  reports: ReportAgg[]
  avoid: boolean
}

export interface CheapestParams {
  lat: number
  lon: number
  radius?: number
  session?: string
  // Custom (user-defined) session — overrides `session` when energy_kwh is set.
  // power_kw omitted/0 means "as fast as the charger allows".
  energy_kwh?: number
  power_kw?: number
  available?: boolean
  min_power?: number
  plug?: string
  limit?: number
}

export interface LiveStatus {
  id: number
  source: 'live' | 'cached' | 'unavailable'
  status: string
  available: boolean
  checked_at: string
  headline_price_eur?: number | null
  currency?: string
}

export const api = {
  cheapest: (p: CheapestParams) =>
    get<{ results: Charger[]; count: number }>('/chargers/cheapest', { ...p }),
  sessions: () => get<{ sessions: SessionProfile[] }>('/sessions'),
  priceHistory: (id: number) =>
    get<{ charger_id: number; history: PricePoint[] }>(`/chargers/${id}/price-history`),
  live: (id: number) => get<LiveStatus>(`/chargers/${id}/live`),
  overview: () => get<Overview>('/stats/overview'),
  trend: (months = 12) => get<{ trend: TrendPoint[] }>('/stats/price-trend', { months }),
  regions: (by = 'city') => get<{ by: string; regions: PriceAgg[] }>('/stats/regions', { by }),
  sessionStats: () => get<{ sessions: SessionStat[] }>('/stats/sessions'),
  reports: (id: number) => get<ReportsResult>(`/chargers/${id}/reports`),
  addReport: (id: number, type: string, value?: ReportValue) =>
    post<ReportsResult>(`/chargers/${id}/reports`, { type, value, client_id: clientId() }),
}
