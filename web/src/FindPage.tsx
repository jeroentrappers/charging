import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { api, type Charger } from './api'
import { MapView } from './MapView'
import { AvailBadge, availOf, eur, km, priceOf, type Filters } from './ui'

const ChargerDetail = lazy(() => import('./ChargerDetail').then((m) => ({ default: m.ChargerDetail })))

interface Viewport { lat: number; lon: number; radius: number }

export function FindPage(props: {
  initial: [number, number]
  located: [number, number] | null // live geolocation, or null if unavailable
  geoNonce: number // bumps on each explicit "Locate me"
  sessionKey: string | undefined
  filters: Filters
}) {
  const [vp, setVp] = useState<Viewport | null>(null)
  const [manualOrigin, setManualOrigin] = useState<[number, number] | null>(null)
  const [chargers, setChargers] = useState<Charger[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [detailId, setDetailId] = useState<number | null>(null)
  const [focusNonce, setFocusNonce] = useState(0)
  const [expanded, setExpanded] = useState(false)

  // "Locate me" re-follows geolocation: clear any manually-dropped pin.
  const lastGeo = useRef(props.geoNonce)
  useEffect(() => {
    if (props.geoNonce !== lastGeo.current) {
      lastGeo.current = props.geoNonce
      setManualOrigin(null)
    }
  }, [props.geoNonce])

  // The search origin: a manually-dropped pin wins, else live geolocation, else
  // the map centre (so the app works before any location is set).
  const origin: [number, number] =
    manualOrigin ?? props.located ?? (vp ? [vp.lat, vp.lon] : props.initial)
  const hasFix = manualOrigin != null || props.located != null
  const oLat = origin[0]
  const oLon = origin[1]
  const radius = vp?.radius ?? 5000

  useEffect(() => {
    const t = setTimeout(async () => {
      setLoading(true)
      setError(false)
      try {
        const r = await api.cheapest({
          lat: oLat,
          lon: oLon,
          radius,
          session: props.sessionKey,
          available: props.filters.available,
          min_power: props.filters.minPower || undefined,
          plug: props.filters.plug || undefined,
          limit: 100,
        })
        setChargers(r.results)
      } catch {
        setError(true)
      } finally {
        setLoading(false)
      }
    }, 300)
    return () => clearTimeout(t)
  }, [oLat, oLon, radius, props.sessionKey, props.filters])

  const detail = detailId != null ? chargers.find((c) => c.id === detailId) ?? null : null

  function select(id: number) {
    setSelectedId(id)
    setDetailId(id)
    setFocusNonce((n) => n + 1)
  }

  const selected = selectedId != null ? chargers.find((c) => c.id === selectedId) ?? null : null
  const focus: [number, number] | null = selected ? [selected.lat, selected.lon] : null

  return (
    <div className="find">
      <MapView
        initial={props.initial}
        recenterTo={props.located}
        recenterNonce={props.geoNonce}
        focus={focus}
        focusNonce={focusNonce}
        origin={origin}
        showOrigin={hasFix}
        chargers={chargers}
        selectedId={selectedId}
        onSelect={select}
        onViewport={(lat, lon, r) => setVp({ lat, lon, radius: r })}
        onPick={(lat, lon) => setManualOrigin([lat, lon])}
      />

      {!hasFix && <div className="map-hint">Tap the map to set your location</div>}

      <div className={`sheet ${expanded ? 'expanded' : ''}`}>
        <div className="handle"><button aria-label="toggle list" onClick={() => setExpanded((e) => !e)} /></div>
        <div className="sheet-head">
          <h2>{loading ? 'Searching…' : `${chargers.length} chargers`}</h2>
          <span className="muted">cheapest first</span>
        </div>
        <div className="list">
          {error && <div className="state">Couldn't load chargers. Check your connection.</div>}
          {!error && !loading && chargers.length === 0 && (
            <div className="state">
              No chargers here{props.filters.available ? ' that are free right now' : ''}.<br />
              Try zooming out{props.filters.available ? ' or turning off "Available now"' : ''}.
            </div>
          )}
          {chargers.map((c) => (
            <button key={c.id} className={`row ${c.id === selectedId ? 'sel' : ''}`} onClick={() => select(c.id)}>
              <span className="price">{eur(priceOf(c))}</span>
              <span>
                <div className="name">{c.name || c.cpo_id}</div>
                <div className="sub">{c.power_kw} kW {c.current_type} · {km(c.distance_m)}</div>
              </span>
              <span className="right"><AvailBadge a={availOf(c)} /></span>
            </button>
          ))}
          {chargers.length > 0 && <p className="caveat">Drive-up (ad-hoc) prices — your charge card may differ. Data: transportdata.be / AFIR.</p>}
        </div>
      </div>

      {detail && (
        <Suspense fallback={null}>
          <ChargerDetail charger={detail} onClose={() => setDetailId(null)} />
        </Suspense>
      )}
    </div>
  )
}
