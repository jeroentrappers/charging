// Client-side pricing — a faithful port of internal/pricing (Go). The server
// returns the nearest candidates with their structured tariff; we compute the
// per-car, time-of-day price + detour and rank here, so changing the car /
// charging profile / detour re-ranks instantly with no network round-trip.
//
// KEEP IN SYNC with internal/pricing/pricing.go + profiles.go (the canonical
// implementation): evaluate/customPrice/detour must match the server's
// /chargers/cheapest. (Two deliberate client-only refinements the server
// doesn't apply: membership/MSP pricing below, and the car's AC/DC max-power cap
// in customPrice — the server only gets usable_kWh + consumption, not max powers.)
import type { Charger, TariffStruct, TariffElement, TariffRestrictions } from './api'
import type { Settings } from './settings'
import { MSPS, flatSessionPrice, markupSessionPrice } from './msps'

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
// carMaxKW (>0) caps the real power the car can draw for this current type —
// e.g. an ID. Buzz tops out ~185 kW DC / 11 kW AC, so a 350 kW charger doesn't
// make its session any faster (a client-only refinement; see the sync note).
function customPrice(t: TariffStruct, chargerPowerKW: number, current: string, batteryKWh: number, powerKW: number, carMaxKW: number, at: Date): number | null {
  if (batteryKWh <= 0 || chargerPowerKW <= 0) return null
  let power = chargerPowerKW
  if (powerKW > 0) {
    if (chargerPowerKW < powerKW - 0.5) return null // charger can't deliver the requested speed
    power = powerKW
  }
  if (carMaxKW > 0 && power > carMaxKW) power = carMaxKW // the car can't draw more than this
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
  const memberships = (settings.memberships ?? []).map((id) => MSPS.find((m) => m.id === id)).filter(Boolean)
  const ranked = chargers.map((c) => {
    const carMaxKW = (c.current_type === 'DC' ? settings.car.maxDcKw : settings.car.maxAcKw) ?? 0
    const adhoc = c.price_components
      ? customPrice(c.price_components, c.power_kw, c.current_type, settings.charge.kWh, settings.charge.powerKW ?? 0, carMaxKW, now)
      : null
    // "Show my real card price": when the user has selected card(s), the price is
    // what THEY would pay with their cheapest selected card — a flat card's
    // blended rate, or a markup card's station-real ad-hoc price + its fee
    // (computed on the fly, not a guess). Bare ad-hoc is the fallback only when no
    // selected card prices this station. A card only prices a charger we actually
    // have a tariff for — we never fabricate a price for location-only chargers
    // (e.g. the DE/FR registries), which would flatten the ranking.
    let price = adhoc
    let via: string | undefined
    let estimated = false
    if (c.price_components && memberships.length > 0) {
      let best: number | null = null
      let bestVia: string | undefined
      let bestEst = false
      for (const mspOrNull of memberships) {
        const m = mspOrNull!
        let mp: number | null = null
        let est = false
        if (m.kind === 'markup') {
          if (adhoc == null) continue // a markup needs the station's real price
          mp = markupSessionPrice(m, adhoc, c.current_type, settings.charge.kWh)
        } else {
          mp = flatSessionPrice(m, c.current_type, settings.charge.kWh)
          est = true
        }
        if (mp != null && (best == null || mp < best)) {
          best = mp
          bestVia = m.name
          bestEst = est
        }
      }
      if (best != null) {
        price = best
        via = bestVia
        estimated = bestEst
      }
    }
    const det = price != null ? detourCost(c.distance_m, settings) : 0
    const weighted = price == null ? null : price + det
    return {
      ...c,
      session_price_eur: price,
      price_via: via,
      price_estimated: estimated || undefined,
      detour_eur: det > 0 ? det : undefined,
      _weighted: weighted,
    }
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
