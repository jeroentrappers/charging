// Lightweight place search for the trip destination, via the public OSM
// Nominatim service. Low-volume, interactive use only (debounced in the UI).
// Biased to Belgium; falls back to anywhere if nothing matches.
export interface Place {
  label: string
  lat: number
  lon: number
}

// Trim a verbose Nominatim label to its first couple of parts.
export function shortPlace(label: string): string {
  return label.split(',').slice(0, 2).join(',').trim()
}

// Reverse-geocode a coordinate to a concise "road, city" label (best-effort,
// low-volume — only when the located fix changes).
export async function reverseGeocode(lat: number, lon: number, signal?: AbortSignal): Promise<string> {
  const url = new URL('https://nominatim.openstreetmap.org/reverse')
  url.searchParams.set('lat', String(lat))
  url.searchParams.set('lon', String(lon))
  url.searchParams.set('format', 'jsonv2')
  url.searchParams.set('zoom', '16') // street level
  url.searchParams.set('addressdetails', '1')
  const res = await fetch(url.toString(), { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) return ''
  const r: { display_name?: string; address?: Record<string, string> } = await res.json()
  const a = r.address || {}
  const road = a.road || a.pedestrian || a.footway || a.suburb || a.neighbourhood || ''
  const city = a.city || a.town || a.village || a.municipality || a.county || ''
  const concise = [road, city].filter(Boolean).join(', ')
  return concise || shortPlace(r.display_name || '')
}

export async function geocode(query: string, signal?: AbortSignal): Promise<Place[]> {
  const q = query.trim()
  if (q.length < 3) return []
  const url = new URL('https://nominatim.openstreetmap.org/search')
  url.searchParams.set('q', q)
  url.searchParams.set('format', 'jsonv2')
  url.searchParams.set('limit', '5')
  url.searchParams.set('countrycodes', 'be,nl,fr,de,lu')
  url.searchParams.set('addressdetails', '0')
  const res = await fetch(url.toString(), {
    signal,
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) return []
  const rows: { display_name: string; lat: string; lon: string }[] = await res.json()
  return rows.map((r) => ({ label: r.display_name, lat: Number(r.lat), lon: Number(r.lon) }))
}
