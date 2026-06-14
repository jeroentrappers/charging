// Mobility Service Providers (roaming/charge cards). Many drivers don't pay the
// station's ad-hoc price — they pay their card's blended tariff, often a flat
// €/kWh regardless of operator. Picking your card(s) lets us show *your* price.
//
// IMPORTANT: these rates are ESTIMATES (rounded, illustrative blended rates) so
// the feature is useful today — they are NOT official and vary by operator and
// over time. Every entry is flagged `estimated`, surfaced as such in the UI, and
// should be replaced with verified/real per-CPO data when a source is available.
// (This is the app's data-honesty rule applied: useful, but never pretend-exact.)
export interface MSP {
  id: string
  name: string
  acEurKWh: number // blended €/kWh on AC
  dcEurKWh: number // blended €/kWh on DC (fast)
  sessionFee: number // € per session
  monthlyFee?: number // € per month (informational; not added to a session)
  estimated: boolean
}

export const MSPS: MSP[] = [
  { id: 'flat-cheap', name: 'Flat card (cheaper AC)', acEurKWh: 0.39, dcEurKWh: 0.59, sessionFee: 0, estimated: true },
  // Mobiflow (BE): no activation/subscription; quotes avg AC €0.50, DC €0.65 on
  // its own network, roaming tariffs vary. https://mobiflow.be/nl/faq/hoeveel-kost-de-mobiflow-laadpas/
  { id: 'mobiflow', name: 'Mobiflow', acEurKWh: 0.5, dcEurKWh: 0.65, sessionFee: 0, estimated: true },
  // Stroohm (BE): €0.05/kWh + €0.20/session on top of the CPO price. Modelled on
  // typical BE public averages (AC €0.40, DC €0.65) + the markup. https://www.stroohm.be/help/hoeveel-kost-publiek-laden/
  { id: 'stroohm', name: 'Stroohm', acEurKWh: 0.45, dcEurKWh: 0.7, sessionFee: 0.2, estimated: true },
  { id: 'shell-recharge', name: 'Shell Recharge', acEurKWh: 0.55, dcEurKWh: 0.69, sessionFee: 0, estimated: true },
  { id: 'chargemap', name: 'Chargemap Pass', acEurKWh: 0.5, dcEurKWh: 0.65, sessionFee: 0.5, estimated: true },
  { id: 'plugsurfing', name: 'Plugsurfing', acEurKWh: 0.55, dcEurKWh: 0.69, sessionFee: 0, estimated: true },
  { id: 'mobilize', name: 'Mobilize / Renault', acEurKWh: 0.45, dcEurKWh: 0.65, sessionFee: 0, estimated: true },
  { id: 'tesla-nonowner', name: 'Tesla (non-owner)', acEurKWh: 0.45, dcEurKWh: 0.55, sessionFee: 0, estimated: true },
]

const EFF_AC = 0.89
const EFF_DC = 0.94

// What a session costs on this card: blended €/kWh on the metered energy
// (battery energy ÷ charging efficiency) plus any per-session fee. Independent
// of the station's own tariff — which is exactly how a flat roaming card works.
export function memberSessionPrice(msp: MSP, current: string, batteryKWh: number): number {
  const metered = batteryKWh / (current === 'DC' ? EFF_DC : EFF_AC)
  const rate = current === 'DC' ? msp.dcEurKWh : msp.acEurKWh
  return metered * rate + msp.sessionFee
}
