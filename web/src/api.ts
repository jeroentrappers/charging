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
  detour_eur?: number | null // estimated round-trip detour cost added to the ranking
  price_components?: TariffStruct // structured tariff (from /chargers/nearby, for client-side pricing)
  price_updated_at?: string | null // when the tariff was last confirmed
  source?: string // operator / data-source name
  source_type?: string // ocpi (direct) | road | monta | … → confidence
  evse_uid?: string
  // Cluster fields: when co-located same-power chargers are grouped, this row
  // represents the group. group_total absent/0 means it's a single charger.
  group_total?: number
  group_available?: number
  group_busy?: number
  members?: ClusterMember[]
  // Client-side (set by pricing.ts when a membership beats the ad-hoc price):
  price_via?: string // membership/MSP name the effective price came from
  price_estimated?: boolean // the winning price is an estimated membership rate
}

// One charger inside a location+power cluster (for drill-down).
export interface ClusterMember {
  id: number
  evse_uid?: string
  status: 'free' | 'busy' | 'unknown'
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

// One source's operational health (mirrors the /status endpoint). Timestamps
// are null when the source has never reported that signal.
export interface SourceHealth {
  id: string
  name: string
  type: string
  country: string
  enabled: boolean
  chargers: number
  priced: number
  available: number
  newest_status: string | null
  // Most recent time a current price was CONFIRMED (re-observed by a pass),
  // not necessarily changed — a stable price reads fresh, not stale.
  newest_price: string | null
  last_run_at: string | null
  last_run_error: string
}

export interface StatusResponse {
  generated: string
  totals: { sources: number; chargers: number; priced: number; available: number }
  sources: SourceHealth[]
}

// One row in the paginated explorer view (/chargers). Flatter than Charger
// because the table is a list, not a map; no distance, no cluster fields, no
// structured tariff.
export interface ChargerListRow {
  id: number
  cpo_id: string
  source: string // cpo.name
  source_type: string
  country: string
  evse_uid: string
  connector_id: string
  name: string
  address: string
  postal_code: string
  city: string
  lat: number
  lon: number
  power_kw: number
  plug_type: string
  current_type: string
  private: boolean
  available_count: number | null
  status_updated_at: string | null
  comparable_price_eur: number | null
  price_updated_at: string | null
}

export interface ChargerListResponse {
  total: number // unpaginated total under the same filter (for "Page X of Y")
  limit: number
  offset: number
  results: ChargerListRow[]
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

// track records a first-party client analytics event. Fire-and-forget and
// wrapped in try/catch so analytics can never break or slow the app; uses
// sendBeacon when available (survives page unload) and never awaits.
export function track(event: string, props?: Record<string, unknown>): void {
  try {
    const body = JSON.stringify({ event, client_id: clientId(), props })
    const url = BASE + '/events'
    if (navigator.sendBeacon) {
      navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }))
    } else {
      void fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body, keepalive: true }).catch(() => {})
    }
  } catch {
    /* analytics must never throw into the UI */
  }
}

// AnalyticsSummary mirrors the /admin/analytics rollup.
export interface AnalyticsSummary {
  since: string
  events: number
  unique_visitors: number
  feed_consumers: number
  top_events: { key: string; count: number }[]
  top_endpoints: { key: string; count: number }[]
  downloads_by_format: { key: string; count: number }[]
  events_per_day: { day: string; count: number }[]
}

// fetchAnalytics calls the admin rollup with a bearer token. Throws on 401 so
// the dashboard can prompt for a valid token.
export async function fetchAnalytics(token: string, days: number): Promise<AnalyticsSummary> {
  const res = await fetch(`${BASE}/admin/analytics?days=${days}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error(`API ${res.status}`)
  return res.json() as Promise<AnalyticsSummary>
}

// fetchNginxReport returns the GoAccess traffic report HTML (admin-gated), for
// rendering in an <iframe srcDoc>. Throws on 401 so the dashboard can re-prompt.
export async function fetchNginxReport(token: string): Promise<string> {
  const res = await fetch(`${BASE}/admin/nginx-report`, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error(`API ${res.status}`)
  return res.text()
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
  // Car parameters for the price calc.
  usable_kwh?: number
  consumption_kwh100?: number
  // Detour weighting (round-trip cost added to the ranking).
  detour?: boolean
  detour_price?: number
  detour_eur_per_h?: number
  available?: boolean
  include_private?: boolean
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

export interface NearbyParams {
  lat: number
  lon: number
  radius?: number
  available?: boolean
  include_private?: boolean
  min_power?: number
  plug?: string
  limit?: number
}

export interface AlongRouteParams {
  from_lat: number
  from_lon: number
  to_lat: number
  to_lon: number
  buffer?: number
  available?: boolean
  include_private?: boolean
  min_power?: number
  plug?: string
  limit?: number
}

export interface RouteGeometry {
  points: { lat: number; lon: number }[]
  distance_m: number
  duration_s: number
}

export interface ChargerListParams {
  q?: string
  source?: string
  country?: string
  plug?: string
  current?: 'AC' | 'DC'
  min_power?: number
  max_power?: number
  available?: boolean
  has_price?: boolean
  include_private?: boolean
  sort?: 'id' | 'name' | 'city' | 'power' | 'plug' | 'current' | 'price' | 'available' | 'source' | 'updated'
  desc?: boolean
  limit?: number
  offset?: number
}

export const api = {
  cheapest: (p: CheapestParams) =>
    get<{ results: Charger[]; count: number }>('/chargers/cheapest', { ...p }),
  // Geo-only candidates (with structured tariffs) for client-side pricing/ranking.
  nearby: (p: NearbyParams) =>
    get<{ results: Charger[]; count: number }>('/chargers/nearby', { ...p }),
  // Corridor search: chargers along the driving route from→to (off-route
  // distance in distance_m), plus the route polyline to draw.
  alongRoute: (p: AlongRouteParams) =>
    get<{ route: RouteGeometry | null; results: Charger[]; count: number }>('/chargers/along-route', { ...p }),
  sessions: () => get<{ sessions: SessionProfile[] }>('/sessions'),
  priceHistory: (id: number) =>
    get<{ charger_id: number; history: PricePoint[] }>(`/chargers/${id}/price-history`),
  live: (id: number) => get<LiveStatus>(`/chargers/${id}/live`),
  charger: (id: number, lat?: number, lon?: number, usable_kwh?: number, consumption_kwh100?: number) =>
    get<Charger>(`/chargers/${id}`, { lat, lon, usable_kwh, consumption_kwh100 }),
  overview: () => get<Overview>('/stats/overview'),
  trend: (months = 12) => get<{ trend: TrendPoint[] }>('/stats/price-trend', { months }),
  regions: (by = 'city') => get<{ by: string; regions: PriceAgg[] }>('/stats/regions', { by }),
  sessionStats: () => get<{ sessions: SessionStat[] }>('/stats/sessions'),
  reports: (id: number) => get<ReportsResult>(`/chargers/${id}/reports`),
  status: () => get<StatusResponse>('/status'),
  // Explorer: paginated/sortable/filterable/searchable list of every charger.
  chargerList: (p: ChargerListParams) => get<ChargerListResponse>('/chargers', { ...p }),
  addReport: (id: number, type: string, value?: ReportValue) =>
    post<ReportsResult>(`/chargers/${id}/reports`, { type, value, client_id: clientId() }),
}
