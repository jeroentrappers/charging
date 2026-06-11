import { useEffect, useState } from 'react'

// User settings, persisted in the browser (localStorage). These drive the price
// comparison: the car determines charging losses + detour energy, the charge
// profile is "how much energy, how fast", and detour adds the cost of driving
// out of the way.
export interface Settings {
  car: {
    usableKWh: number
    consumptionKWh100: number
    modelId?: string // picked CarModel id, or undefined for a custom/manual car
    plugs?: string[] // canonical OCPI plug types the car accepts (for compat filter)
    maxAcKw?: number
    maxDcKw?: number
  }
  charge: { kWh: number; powerKW: number | null } // energy to add; powerKW null = as fast as possible
  detour: { enabled: boolean; refPrice: number; eurPerHour: number }
  memberships: string[] // selected MSP ids; prices use the cheapest of ad-hoc vs these
}

export const DEFAULT_SETTINGS: Settings = {
  car: { usableKWh: 60, consumptionKWh100: 18 },
  charge: { kWh: 40, powerKW: null },
  detour: { enabled: true, refPrice: 0.3, eurPerHour: 12 },
  memberships: [],
}

const KEY = 'charging.settings'

function load(): Settings {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return DEFAULT_SETTINGS
    const p = JSON.parse(raw)
    return {
      car: { ...DEFAULT_SETTINGS.car, ...p.car },
      charge: { ...DEFAULT_SETTINGS.charge, ...p.charge },
      detour: { ...DEFAULT_SETTINGS.detour, ...p.detour },
      memberships: Array.isArray(p.memberships) ? p.memberships : [],
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}

export function useSettings(): [Settings, (patch: Partial<Settings>) => void] {
  const [s, setS] = useState<Settings>(load)
  useEffect(() => {
    try {
      localStorage.setItem(KEY, JSON.stringify(s))
    } catch {
      /* ignore quota / private mode */
    }
  }, [s])
  return [s, (patch) => setS((cur) => ({ ...cur, ...patch }))]
}

// Quick energy presets derived from the car.
export function energyPresets(car: Settings['car']): { key: string; kWh: number }[] {
  return [
    { key: 'km100', kWh: Math.max(1, Math.round(car.consumptionKWh100)) }, // ~100 km
    { key: 'soc1080', kWh: Math.max(1, Math.round(car.usableKWh * 0.7)) }, // 10→80%
    { key: 'full', kWh: Math.max(1, Math.round(car.usableKWh * 0.9)) }, // 10→100%
  ]
}
