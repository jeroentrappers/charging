// Lightweight place search for the trip destination, via the public OSM
// Nominatim service. Low-volume, interactive use only (debounced in the UI).
// Biased to Belgium; falls back to anywhere if nothing matches.
export interface Place {
  label: string
  lat: number
  lon: number
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
