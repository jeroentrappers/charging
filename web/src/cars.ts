// A curated dataset of EVs common on Belgian roads, so users pick their car
// instead of guessing kWh. Values are usable battery (kWh) and real-world
// consumption (kWh/100 km, ~WLTP+15%), plus typical max AC/DC charge power.
// Not exhaustive and not official — a "Custom" entry always lets users override.
//
// dcStandard drives plug compatibility: EU cars all take Type 2 for AC; the DC
// side is CCS (Combo 2) for almost everything modern, CHAdeMO for a few older
// models. (EU Teslas use CCS2, so they map to 'ccs'.)
export interface CarModel {
  id: string
  make: string
  model: string
  usableKWh: number
  consumptionKWh100: number
  acKw: number
  dcKw: number
  dcStandard: 'ccs' | 'chademo'
}

export const CARS: CarModel[] = [
  // Tesla
  { id: 'tesla-model-3-rwd', make: 'Tesla', model: 'Model 3 RWD', usableKWh: 57.5, consumptionKWh100: 15, acKw: 11, dcKw: 170, dcStandard: 'ccs' },
  { id: 'tesla-model-3-lr', make: 'Tesla', model: 'Model 3 Long Range', usableKWh: 75, consumptionKWh100: 16, acKw: 11, dcKw: 250, dcStandard: 'ccs' },
  { id: 'tesla-model-y-rwd', make: 'Tesla', model: 'Model Y RWD', usableKWh: 60, consumptionKWh100: 16.5, acKw: 11, dcKw: 170, dcStandard: 'ccs' },
  { id: 'tesla-model-y-lr', make: 'Tesla', model: 'Model Y Long Range', usableKWh: 75, consumptionKWh100: 17, acKw: 11, dcKw: 250, dcStandard: 'ccs' },
  // VW group
  { id: 'vw-id3-58', make: 'Volkswagen', model: 'ID.3 Pro (58 kWh)', usableKWh: 58, consumptionKWh100: 16.5, acKw: 11, dcKw: 120, dcStandard: 'ccs' },
  { id: 'vw-id3-77', make: 'Volkswagen', model: 'ID.3 Pro S (77 kWh)', usableKWh: 77, consumptionKWh100: 17, acKw: 11, dcKw: 170, dcStandard: 'ccs' },
  { id: 'vw-id4-77', make: 'Volkswagen', model: 'ID.4 Pro (77 kWh)', usableKWh: 77, consumptionKWh100: 18.5, acKw: 11, dcKw: 135, dcStandard: 'ccs' },
  { id: 'vw-id5-77', make: 'Volkswagen', model: 'ID.5 (77 kWh)', usableKWh: 77, consumptionKWh100: 18, acKw: 11, dcKw: 135, dcStandard: 'ccs' },
  { id: 'vw-id7-77', make: 'Volkswagen', model: 'ID.7 Pro (77 kWh)', usableKWh: 77, consumptionKWh100: 16.5, acKw: 11, dcKw: 175, dcStandard: 'ccs' },
  { id: 'skoda-enyaq-77', make: 'Škoda', model: 'Enyaq 85 (77 kWh)', usableKWh: 77, consumptionKWh100: 17.5, acKw: 11, dcKw: 135, dcStandard: 'ccs' },
  { id: 'cupra-born-58', make: 'Cupra', model: 'Born (58 kWh)', usableKWh: 58, consumptionKWh100: 16.5, acKw: 11, dcKw: 120, dcStandard: 'ccs' },
  { id: 'audi-q4-77', make: 'Audi', model: 'Q4 e-tron (77 kWh)', usableKWh: 77, consumptionKWh100: 18.5, acKw: 11, dcKw: 135, dcStandard: 'ccs' },
  // Hyundai / Kia (800V)
  { id: 'hyundai-ioniq5-77', make: 'Hyundai', model: 'Ioniq 5 (77 kWh)', usableKWh: 74, consumptionKWh100: 18, acKw: 11, dcKw: 233, dcStandard: 'ccs' },
  { id: 'hyundai-ioniq6-77', make: 'Hyundai', model: 'Ioniq 6 (77 kWh)', usableKWh: 74, consumptionKWh100: 15.5, acKw: 11, dcKw: 233, dcStandard: 'ccs' },
  { id: 'hyundai-kona-65', make: 'Hyundai', model: 'Kona Electric (65 kWh)', usableKWh: 65, consumptionKWh100: 16.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'kia-ev6-77', make: 'Kia', model: 'EV6 (77 kWh)', usableKWh: 74, consumptionKWh100: 17.5, acKw: 11, dcKw: 233, dcStandard: 'ccs' },
  { id: 'kia-ev3-81', make: 'Kia', model: 'EV3 (81 kWh)', usableKWh: 81, consumptionKWh100: 16, acKw: 11, dcKw: 128, dcStandard: 'ccs' },
  { id: 'kia-niro-65', make: 'Kia', model: 'Niro EV (65 kWh)', usableKWh: 64.8, consumptionKWh100: 16.5, acKw: 11, dcKw: 80, dcStandard: 'ccs' },
  // Stellantis (e-CMP / STLA)
  { id: 'peugeot-e208-51', make: 'Peugeot', model: 'e-208 (51 kWh)', usableKWh: 48.1, consumptionKWh100: 15.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'peugeot-e2008-51', make: 'Peugeot', model: 'e-2008 (51 kWh)', usableKWh: 48.1, consumptionKWh100: 17, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'opel-corsa-51', make: 'Opel', model: 'Corsa Electric (51 kWh)', usableKWh: 48.1, consumptionKWh100: 15.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'opel-mokka-51', make: 'Opel', model: 'Mokka Electric (51 kWh)', usableKWh: 48.1, consumptionKWh100: 16.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'citroen-ec4-51', make: 'Citroën', model: 'ë-C4 (51 kWh)', usableKWh: 48.1, consumptionKWh100: 16.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'fiat-500e-42', make: 'Fiat', model: '500e (42 kWh)', usableKWh: 37.3, consumptionKWh100: 15, acKw: 11, dcKw: 85, dcStandard: 'ccs' },
  { id: 'jeep-avenger-54', make: 'Jeep', model: 'Avenger (54 kWh)', usableKWh: 50.8, consumptionKWh100: 15.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  // Renault / Dacia
  { id: 'renault-megane-60', make: 'Renault', model: 'Mégane E-Tech (60 kWh)', usableKWh: 60, consumptionKWh100: 16.5, acKw: 22, dcKw: 130, dcStandard: 'ccs' },
  { id: 'renault-scenic-87', make: 'Renault', model: 'Scénic E-Tech (87 kWh)', usableKWh: 87, consumptionKWh100: 17, acKw: 22, dcKw: 150, dcStandard: 'ccs' },
  { id: 'renault-5-52', make: 'Renault', model: '5 E-Tech (52 kWh)', usableKWh: 52, consumptionKWh100: 15, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'renault-zoe-52', make: 'Renault', model: 'Zoe R135 (52 kWh)', usableKWh: 52, consumptionKWh100: 17, acKw: 22, dcKw: 46, dcStandard: 'ccs' },
  { id: 'dacia-spring-27', make: 'Dacia', model: 'Spring (27 kWh)', usableKWh: 26.8, consumptionKWh100: 14.5, acKw: 7, dcKw: 34, dcStandard: 'ccs' },
  // BMW / Mercedes / Volvo / Polestar
  { id: 'bmw-i4-edrive40', make: 'BMW', model: 'i4 eDrive40 (84 kWh)', usableKWh: 80.7, consumptionKWh100: 17, acKw: 11, dcKw: 205, dcStandard: 'ccs' },
  { id: 'bmw-ix1-65', make: 'BMW', model: 'iX1 (65 kWh)', usableKWh: 64.7, consumptionKWh100: 17.5, acKw: 11, dcKw: 130, dcStandard: 'ccs' },
  { id: 'bmw-ix3-74', make: 'BMW', model: 'iX3 (74 kWh)', usableKWh: 74, consumptionKWh100: 18.5, acKw: 11, dcKw: 150, dcStandard: 'ccs' },
  { id: 'mb-eqa-67', make: 'Mercedes', model: 'EQA 250 (67 kWh)', usableKWh: 66.5, consumptionKWh100: 17.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'mb-eqb-67', make: 'Mercedes', model: 'EQB 250 (67 kWh)', usableKWh: 66.5, consumptionKWh100: 18.5, acKw: 11, dcKw: 100, dcStandard: 'ccs' },
  { id: 'volvo-ex30-69', make: 'Volvo', model: 'EX30 (69 kWh)', usableKWh: 64, consumptionKWh100: 16.5, acKw: 11, dcKw: 153, dcStandard: 'ccs' },
  { id: 'volvo-ec40-82', make: 'Volvo', model: 'EC40 (82 kWh)', usableKWh: 79, consumptionKWh100: 18.5, acKw: 11, dcKw: 205, dcStandard: 'ccs' },
  { id: 'polestar-2-82', make: 'Polestar', model: '2 Long Range (82 kWh)', usableKWh: 79, consumptionKWh100: 17.5, acKw: 11, dcKw: 205, dcStandard: 'ccs' },
  // MG / BYD / others
  { id: 'mg4-64', make: 'MG', model: 'MG4 (64 kWh)', usableKWh: 61.7, consumptionKWh100: 16.5, acKw: 11, dcKw: 135, dcStandard: 'ccs' },
  { id: 'mg-zs-ev-69', make: 'MG', model: 'ZS EV (69 kWh)', usableKWh: 68.3, consumptionKWh100: 17.5, acKw: 11, dcKw: 92, dcStandard: 'ccs' },
  { id: 'byd-atto3-60', make: 'BYD', model: 'Atto 3 (60 kWh)', usableKWh: 60.5, consumptionKWh100: 17.5, acKw: 11, dcKw: 88, dcStandard: 'ccs' },
  { id: 'byd-dolphin-60', make: 'BYD', model: 'Dolphin (60 kWh)', usableKWh: 60.4, consumptionKWh100: 16, acKw: 11, dcKw: 88, dcStandard: 'ccs' },
  { id: 'byd-seal-82', make: 'BYD', model: 'Seal (82 kWh)', usableKWh: 82.5, consumptionKWh100: 16.5, acKw: 11, dcKw: 150, dcStandard: 'ccs' },
  { id: 'smart-1-66', make: 'Smart', model: '#1 (66 kWh)', usableKWh: 62, consumptionKWh100: 17.5, acKw: 22, dcKw: 150, dcStandard: 'ccs' },
  // Older / CHAdeMO
  { id: 'nissan-leaf-40', make: 'Nissan', model: 'Leaf (40 kWh)', usableKWh: 39, consumptionKWh100: 17, acKw: 6.6, dcKw: 46, dcStandard: 'chademo' },
  { id: 'nissan-leaf-62', make: 'Nissan', model: 'Leaf e+ (62 kWh)', usableKWh: 59, consumptionKWh100: 18, acKw: 6.6, dcKw: 46, dcStandard: 'chademo' },
  { id: 'nissan-ariya-87', make: 'Nissan', model: 'Ariya (87 kWh)', usableKWh: 87, consumptionKWh100: 18, acKw: 22, dcKw: 130, dcStandard: 'ccs' },
]

// Canonical OCPI plug types a car can use: Type 2 for AC, plus its DC standard.
export function carPlugs(c: Pick<CarModel, 'dcStandard'>): string[] {
  return c.dcStandard === 'chademo'
    ? ['IEC_62196_T2', 'CHADEMO']
    : ['IEC_62196_T2', 'IEC_62196_T2_COMBO']
}

export function carLabel(c: CarModel): string {
  return `${c.make} ${c.model}`
}
