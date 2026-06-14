// Mobility Service Providers (roaming / charge cards). Two kinds, because the
// two bill in fundamentally different ways:
//
//  - 'markup': you pay the STATION's own ad-hoc price plus a per-kWh roaming
//    markup and a per-session fee. We compute the REAL price for each station on
//    the fly from its actual tariff (see pricing.ts) — not a blended guess — so
//    these are not estimates. This is how most BE roaming cards work.
//  - 'flat': the card bills a blended €/kWh regardless of the operator. Useful,
//    but an ESTIMATE (rates vary by operator and over time); flagged `estimated`
//    and surfaced with a ≈ in the UI. (The app's data-honesty rule: useful, but
//    never pretend-exact.)
export interface BaseMSP {
  id: string
  name: string
  sessionFee: number // € per session, added on top
  monthlyFee?: number // € per month (informational; not added to a session)
}

export interface FlatMSP extends BaseMSP {
  kind: 'flat'
  acEurKWh: number // blended €/kWh on AC
  dcEurKWh: number // blended €/kWh on DC (fast)
  estimated: true
}

export interface MarkupMSP extends BaseMSP {
  kind: 'markup'
  markupEurKWh: number // added per metered kWh, on top of the station's ad-hoc price
}

export type MSP = FlatMSP | MarkupMSP

export const MSPS: MSP[] = [
  // --- Markup cards: real price = station ad-hoc + markup, computed per station ---
  // Mobiflow (BE): no activation/subscription; you pay the operator's own price
  // (pass-through), roaming surcharges vary.
  // https://mobiflow.be/nl/faq/hoeveel-kost-de-mobiflow-laadpas/
  { kind: 'markup', id: 'mobiflow', name: 'Mobiflow', markupEurKWh: 0, sessionFee: 0 },
  // Stroohm (BE): €0.05/kWh + €0.20/session on top of the operator's price.
  // https://www.stroohm.be/help/hoeveel-kost-publiek-laden/
  { kind: 'markup', id: 'stroohm', name: 'Stroohm', markupEurKWh: 0.05, sessionFee: 0.2 },
  // --- Flat cards: blended €/kWh independent of the operator (ESTIMATES) ---
  { kind: 'flat', id: 'flat-cheap', name: 'Flat card (cheaper AC)', acEurKWh: 0.39, dcEurKWh: 0.59, sessionFee: 0, estimated: true },
  { kind: 'flat', id: 'shell-recharge', name: 'Shell Recharge', acEurKWh: 0.55, dcEurKWh: 0.69, sessionFee: 0, estimated: true },
  { kind: 'flat', id: 'chargemap', name: 'Chargemap Pass', acEurKWh: 0.5, dcEurKWh: 0.65, sessionFee: 0.5, estimated: true },
  { kind: 'flat', id: 'plugsurfing', name: 'Plugsurfing', acEurKWh: 0.55, dcEurKWh: 0.69, sessionFee: 0, estimated: true },
  { kind: 'flat', id: 'mobilize', name: 'Mobilize / Renault', acEurKWh: 0.45, dcEurKWh: 0.65, sessionFee: 0, estimated: true },
  { kind: 'flat', id: 'tesla-nonowner', name: 'Tesla (non-owner)', acEurKWh: 0.45, dcEurKWh: 0.55, sessionFee: 0, estimated: true },
]

const EFF_AC = 0.89
const EFF_DC = 0.94

// Metered (grid) energy for a given battery top-up — what the card actually
// bills on, since charging losses mean you draw more than lands in the battery.
function metered(current: string, batteryKWh: number): number {
  return batteryKWh / (current === 'DC' ? EFF_DC : EFF_AC)
}

// Flat card: blended €/kWh on the metered energy plus any per-session fee.
// Independent of the station's own tariff — exactly how a flat roaming card bills.
export function flatSessionPrice(msp: FlatMSP, current: string, batteryKWh: number): number {
  const rate = current === 'DC' ? msp.dcEurKWh : msp.acEurKWh
  return metered(current, batteryKWh) * rate + msp.sessionFee
}

// Markup card: the station's REAL ad-hoc session price (computed from its tariff)
// plus a per-kWh roaming markup on the metered energy plus a per-session fee.
export function markupSessionPrice(msp: MarkupMSP, adhoc: number, current: string, batteryKWh: number): number {
  return adhoc + msp.markupEurKWh * metered(current, batteryKWh) + msp.sessionFee
}
