// Client-side pricing — a faithful port of internal/pricing (Go). The server
// returns the nearest candidates with their structured tariff; we compute the
// per-car, time-of-day price + detour and rank here, so changing the car /
// charging profile / detour re-ranks instantly with no network round-trip.
//
// KEEP IN SYNC with internal/pricing/pricing.go + profiles.go (the canonical
// implementation). The numbers must match the server's /chargers/cheapest.
import type { Charger, TariffStruct, TariffElement, TariffRestrictions } from './api'
import type { Settings } from './settings'

const EFF_AC = 0.89
const EFF_DC = 0.94
const DC_TAPER = 0.73

interface Session { kWh: number; power: number; avgPower: number; at: Date }

function duration(s: Session): number {
  const p = s.avgPower > 0 ? s.avgPower : s.power
  return p > 0 ? s.kWh / p : 0
}

function parseHM(s: string | undefined, def: number): number {
  if (!s || !s.includes(':')) return def
  const [h, m] = s.split(':')
  const hh = Number(h), mm = Number(m)
  if (!Number.isFinite(hh) || !Number.isFinite(mm)) return def
  return hh * 60 + mm
}

function withinTimeOfDay(start: string | undefined, end: string | undefined, at: Date): boolean {
  if (!start && !end) return true
  const cur = at.getHours() * 60 + at.getMinutes()
  const s = parseHM(start, 0)
  const e = parseHM(end, 24 * 60)
  return e <= s ? cur >= s || cur < e : cur >= s && cur < e // overnight if e<=s
}

const DAYS = ['SUNDAY', 'MONDAY', 'TUESDAY', 'WEDNESDAY', 'THURSDAY', 'FRIDAY', 'SATURDAY']

function matches(r: TariffRestrictions | undefined, s: Session, hours: number): boolean {
  if (!r) return true
  if (r.day_of_week?.length && !r.day_of_week.some((d) => d.trim().toUpperCase() === DAYS[s.at.getDay()])) return false
  if (!withinTimeOfDay(r.start_time, r.end_time, s.at)) return false
  if (r.min_kwh != null && s.kWh < r.min_kwh) return false
  if (r.max_kwh != null && s.kWh > r.max_kwh) return false
  if (r.min_power != null && s.power < r.min_power) return false
  if (r.max_power != null && s.power > r.max_power) return false
  const secs = hours * 3600
  if (r.min_duration != null && secs < r.min_duration) return false
  if (r.max_duration != null && secs > r.max_duration) return false
  return true
}

function firstPrice(t: TariffStruct, s: Session, hours: number, dim: string): number | null {
  for (const el of t.elements as TariffElement[]) {
    if (!matches(el.restrictions, s, hours)) continue
    for (const pc of el.price_components) if (pc.type === dim) return pc.price
  }
  return null
}

// evaluate mirrors pricing.Evaluate: first matching element per dimension;
// ENERGY×kWh + FLAT + TIME×hours. PARKING_TIME excluded.
function evaluate(t: TariffStruct, s: Session): number | null {
  const hours = duration(s)
  const e = firstPrice(t, s, hours, 'ENERGY')
  const f = firstPrice(t, s, hours, 'FLAT')
  const tm = firstPrice(t, s, hours, 'TIME')
  if (e == null && f == null && tm == null) return null
  let total = 0
  if (e != null) total += e * s.kWh
  if (f != null) total += f
  if (tm != null) total += tm * hours
  return total
}

// customPrice mirrors pricing.CustomPriceAt: a user-defined session (energy +
// optional power cap). powerKW <= 0 means "as fast as the charger allows".
function customPrice(t: TariffStruct, chargerPowerKW: number, current: string, batteryKWh: number, powerKW: number, at: Date): number | null {
  if (batteryKWh <= 0 || chargerPowerKW <= 0) return null
  let power = chargerPowerKW
  if (powerKW > 0) {
    if (chargerPowerKW < powerKW - 0.5) return null // charger can't deliver it
    power = powerKW
  }
  const metered = batteryKWh / (current === 'DC' ? EFF_DC : EFF_AC)
  const avg = current === 'DC' ? power * DC_TAPER : power
  return evaluate(t, { kWh: metered, power, avgPower: avg, at })
}

function detourCost(distanceM: number, settings: Settings): number {
  if (!settings.detour.enabled) return 0
  const rtKm = (2 * distanceM) / 1000
  const energy = (settings.car.consumptionKWh100 * rtKm) / 100 * settings.detour.refPrice
  const time = (rtKm / 50) * settings.detour.eurPerHour
  return energy + time
}

// rankChargers prices each candidate for the user's car + charging profile at
// `now`, adds the detour, and returns the cheapest `limit` by weighted cost
// (avoid-flagged sink to the bottom; unpriceable last). It writes the computed
// price into session_price_eur and detour_eur so the existing UI renders it.
export function rankChargers(chargers: Charger[], settings: Settings, now: Date, limit = 50): Charger[] {
  const ranked = chargers.map((c) => {
    const price = c.price_components
      ? customPrice(c.price_components, c.power_kw, c.current_type, settings.charge.kWh, settings.charge.powerKW ?? 0, now)
      : null
    const det = price != null ? detourCost(c.distance_m, settings) : 0
    const weighted = price == null ? null : price + det
    return { ...c, session_price_eur: price, detour_eur: det > 0 ? det : undefined, _weighted: weighted }
  })
  ranked.sort((a, b) => {
    if (!!a.avoid !== !!b.avoid) return a.avoid ? 1 : -1
    const wa = a._weighted, wb = b._weighted
    if ((wa == null) !== (wb == null)) return wa == null ? 1 : -1
    if (wa != null && wb != null && wa !== wb) return wa - wb
    return a.distance_m - b.distance_m
  })
  return ranked.slice(0, limit).map(({ _weighted, ...c }) => c)
}
