import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Charger, type RouteGeometry } from './api'
import { MapView } from './MapView'
import { rankChargers } from './pricing'
import { geocode, shortPlace, type Place } from './geocode'
import type { NavState } from './url'
import type { Settings } from './settings'
import { AvailBadge, availOf, eur, km, plugLabel, priceOf, type Filters } from './ui'

const ChargerDetail = lazy(() => import('./ChargerDetail').then((m) => ({ default: m.ChargerDetail })))

// The list is the cheapest chargers in a generous region around the origin —
// deliberately NOT tied to the visible map area, so zooming/panning the map
// doesn't shrink or grow it.
const SEARCH_RADIUS_M = 50000
const CANDIDATE_POOL = 200 // nearest candidates fetched; the client ranks + trims
const RESULT_LIMIT = 50

interface Viewport { lat: number; lon: number }

export function FindPage(props: {
  fallbackCenter: [number, number] // used until geolocation / a URL center is known
  located: [number, number] | null // live geolocation, or null if unavailable
  accuracy: number | null // GPS accuracy radius (m), for the geolocated pin
  geoNonce: number // bumps on each explicit "Locate me"
  route: NavState // current URL (center + open charger)
  routeNonce: number // bumps on load + back/forward, to (re)apply the URL
  onCenter: (c: { lat: number; lon: number; zoom: number }) => void
  onOpenCharger: (c: Charger) => void
  onCloseCharger: () => void
  settings: Settings // car + charging profile + detour weighting
  filters: Filters
  tripTo: Place | null // corridor destination (lifted to App so it shows in the header)
  onSetTrip: (p: Place) => void
  onClearTrip: () => void
}) {
  const { t } = useTranslation()
  const [vp, setVp] = useState<Viewport | null>(null)
  const [manualOrigin, setManualOrigin] = useState<[number, number] | null>(null)
  const [raw, setRaw] = useState<Charger[]>([]) // geo candidates from the server
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  // The open detail is a *snapshot* of the selected charger, not a lookup into
  // the live results — so it survives a query refresh that no longer includes it.
  const [detailCharger, setDetailCharger] = useState<Charger | null>(null)
  const [focusNonce, setFocusNonce] = useState(0)
  // Programmatic map recenter (applying a URL on load / back-forward).
  const [view, setView] = useState<{ to: [number, number]; zoom?: number } | null>(null)
  const [viewNonce, setViewNonce] = useState(0)
  const [expanded, setExpanded] = useState(false)
  // Trip / corridor mode: a destination (props.tripTo, owned by App) turns the
  // list into "chargers along the way", ranked by price + deviation from route.
  const tripTo = props.tripTo
  const [route, setRoute] = useState<RouteGeometry | null>(null)
  const [routeNonce, setRouteNonce] = useState(0)

  // Set synchronously when a detail is (logically) open, so the map-move handler
  // never writes a /@center URL over a /charger/{id} URL during a recenter.
  const detailOpenRef = useRef(false)

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
  const routeCenter = props.route.center
  const fallback: [number, number] = routeCenter ? [routeCenter.lat, routeCenter.lon] : props.fallbackCenter
  const origin: [number, number] = manualOrigin ?? props.located ?? (vp ? [vp.lat, vp.lon] : fallback)
  const hasFix = manualOrigin != null || props.located != null
  const accuracyM = manualOrigin == null && props.located != null ? props.accuracy : null
  const oLat = origin[0]
  const oLon = origin[1]
  const radius = SEARCH_RADIUS_M

  // Latest values for the (nonce-keyed) URL-apply effect, without making them deps.
  const candidatesRef = useRef(raw)
  candidatesRef.current = raw
  const originRef = useRef(origin)
  originRef.current = origin

  // Fetch the geo candidate pool from the server — only when the origin, radius
  // or filters change. Pricing/detour/ranking happens client-side below, so
  // tweaking the car / charging profile / detour never refetches.
  useEffect(() => {
    const handle = setTimeout(async () => {
      setLoading(true)
      setError(false)
      const f = props.filters
      try {
        if (tripTo) {
          // Corridor search: chargers along the route from origin to destination.
          const r = await api.alongRoute({
            from_lat: oLat,
            from_lon: oLon,
            to_lat: tripTo.lat,
            to_lon: tripTo.lon,
            buffer: 4000,
            available: f.available,
            include_private: f.includePrivate,
            min_power: f.minPower || undefined,
            plug: f.plug || undefined,
            limit: 80,
          })
          setRaw(r.results)
          setRoute(r.route)
          setRouteNonce((n) => n + 1)
        } else {
          const r = await api.nearby({
            lat: oLat,
            lon: oLon,
            radius,
            available: f.available,
            include_private: f.includePrivate,
            min_power: f.minPower || undefined,
            plug: f.plug || undefined,
            limit: CANDIDATE_POOL,
          })
          setRaw(r.results)
          setRoute(null)
        }
      } catch {
        setError(true)
      } finally {
        setLoading(false)
      }
    }, 300)
    return () => clearTimeout(handle)
  }, [oLat, oLon, radius, props.filters, tripTo])

  // Price + detour + rank entirely on the client; re-ranks instantly when the
  // car / charging profile / detour settings change (no network round-trip).
  // "Fits my car" narrows the pool to plugs the selected car accepts.
  const chargers = useMemo(() => {
    const plugs = props.settings.car.plugs
    const pool =
      props.filters.plugCompatible && plugs && plugs.length > 0
        ? raw.filter((c) => plugs.includes(c.plug_type))
        : raw
    return rankChargers(pool, props.settings, new Date(), RESULT_LIMIT)
  }, [raw, props.settings, props.filters.plugCompatible])

  // Refresh the open detail's data from new results, but keep the snapshot if
  // the charger isn't in the latest set.
  useEffect(() => {
    setDetailCharger((prev) => (prev ? chargers.find((c) => c.id === prev.id) ?? prev : null))
  }, [chargers])

  // Apply the URL to the map + open charger on load and on back/forward.
  useEffect(() => {
    if (props.route.center) {
      setView({ to: [props.route.center.lat, props.route.center.lon], zoom: props.route.center.zoom })
      setViewNonce((n) => n + 1)
    }
    const id = props.route.chargerId
    if (id == null) {
      detailOpenRef.current = false
      setDetailCharger(null)
      setSelectedId(null)
      return
    }
    detailOpenRef.current = true
    setSelectedId(id)
    const inList = candidatesRef.current.find((c) => c.id === id)
    if (inList) {
      setDetailCharger(inList)
      setFocusNonce((n) => n + 1)
    } else {
      api
        .charger(id, originRef.current[0], originRef.current[1], props.settings.car.usableKWh, props.settings.car.consumptionKWh100)
        .then((c) => {
          setDetailCharger(c)
          setView({ to: [c.lat, c.lon] })
          setViewNonce((n) => n + 1)
        })
        .catch(() => {})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.routeNonce])

  function select(id: number) {
    const c = chargers.find((x) => x.id === id) ?? null
    detailOpenRef.current = true
    setSelectedId(id)
    setDetailCharger(c)
    setFocusNonce((n) => n + 1)
    if (c) props.onOpenCharger(c)
  }
  function closeDetail() {
    detailOpenRef.current = false
    setDetailCharger(null)
    setSelectedId(null)
    props.onCloseCharger()
  }

  function onViewport(lat: number, lon: number, _r: number, zoom: number) {
    setVp({ lat, lon })
    // Reflect the map position in the URL — but not while a charger detail owns
    // the URL (/charger/{id}).
    if (!detailOpenRef.current) props.onCenter({ lat, lon, zoom })
  }

  // Focus the map on the open charger (from the snapshot, so it's always known).
  const focus: [number, number] | null = detailCharger ? [detailCharger.lat, detailCharger.lon] : null
  // Keep the selected charger's marker on the map even if it left the results.
  const mapChargers =
    detailCharger && !chargers.some((c) => c.id === detailCharger.id) ? [...chargers, detailCharger] : chargers

  return (
    <div className="find">
      <MapView
        initial={fallback}
        initialZoom={routeCenter?.zoom}
        recenterTo={props.located}
        recenterNonce={props.geoNonce}
        viewTo={view?.to ?? null}
        viewZoom={view?.zoom}
        viewNonce={viewNonce}
        focus={focus}
        focusNonce={focusNonce}
        origin={origin}
        showOrigin={hasFix}
        accuracyM={accuracyM}
        chargers={mapChargers}
        selectedId={selectedId}
        onSelect={select}
        onViewport={onViewport}
        onPick={(lat, lon) => setManualOrigin([lat, lon])}
        route={route ? route.points.map((p) => [p.lat, p.lon] as [number, number]) : null}
        dest={tripTo ? [tripTo.lat, tripTo.lon] : null}
        routeNonce={routeNonce}
      />

      {!hasFix && <div className="map-hint">{t('find.tapHint')}</div>}

      <div className={`sheet ${expanded ? 'expanded' : ''}`}>
        <div className="handle"><button aria-label="toggle list" onClick={() => setExpanded((e) => !e)} /></div>
        <TripBar dest={tripTo} route={route} settings={props.settings} onSet={props.onSetTrip} onClear={props.onClearTrip} />
        <div className="sheet-head">
          <h2>{loading ? t('find.searching') : t('find.chargers', { count: chargers.length })}</h2>
          <span className="muted">{tripTo ? t('find.alongRoute') : t('find.cheapestFirst')}</span>
        </div>
        <div className="list">
          {error && <div className="state">{t('find.loadError')}</div>}
          {!error && !loading && chargers.length === 0 && (
            <div className="state">{props.filters.available ? t('find.emptyAvailable') : t('find.empty')}</div>
          )}
          {chargers.map((c) => (
            <button key={c.id} className={`row ${c.id === selectedId ? 'sel' : ''}`} onClick={() => select(c.id)}>
              <span className="price">
                {eur(priceOf(c))}
                {c.price_via && <span className="price-via">{c.price_estimated ? '≈' : ''} {c.price_via}</span>}
              </span>
              <span>
                <div className="name">{c.name || c.cpo_id}</div>
                <div className="sub">
                  {c.power_kw} kW {c.current_type} · {plugLabel(c.plug_type)} · {km(c.distance_m)}
                  {c.detour_eur != null && c.detour_eur > 0 && <span className="detour"> · +{eur(c.detour_eur)} {t('find.detour')}</span>}
                  {c.avoid && <span className="flag-badge"> · ⚠ {t('report.flagged')}</span>}
                </div>
              </span>
              <span className="right"><AvailBadge a={availOf(c)} /></span>
            </button>
          ))}
          {chargers.length > 0 && <p className="caveat">{t('find.caveat')}</p>}
        </div>
      </div>

      {detailCharger && (
        <Suspense fallback={null}>
          <ChargerDetail charger={detailCharger} onClose={closeDetail} />
        </Suspense>
      )}
    </div>
  )
}

// Trip destination search + summary. When a destination is set, the list shows
// chargers along the route; the summary shows trip distance/time and whether it
// fits the car's range.
function TripBar(props: {
  dest: Place | null
  route: RouteGeometry | null
  settings: Settings
  onSet: (p: Place) => void
  onClear: () => void
}) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Place[]>([])

  useEffect(() => {
    if (props.dest || q.trim().length < 3) {
      setResults([])
      return
    }
    const ctrl = new AbortController()
    const h = setTimeout(() => {
      geocode(q, ctrl.signal).then(setResults).catch(() => {})
    }, 350)
    return () => {
      clearTimeout(h)
      ctrl.abort()
    }
  }, [q, props.dest])

  if (props.dest) {
    const r = props.route
    const tripKm = r ? Math.round(r.distance_m / 1000) : null
    const tripMin = r ? Math.round(r.duration_s / 60) : null
    const { usableKWh, consumptionKWh100 } = props.settings.car
    const rangeKm = Math.round((usableKWh / consumptionKWh100) * 100)
    const within = tripKm != null ? tripKm <= rangeKm : true
    return (
      <div className="tripbar set">
        <span className="trip-dest">🏁 {shortPlace(props.dest.label)}</span>
        {tripKm != null && (
          <span className="trip-stats">
            {t('trip.distance', { km: tripKm, min: tripMin })} ·{' '}
            <span className={within ? 'ok' : 'warn'}>{t('trip.range', { km: rangeKm })} {within ? '✓' : '⚠'}</span>
          </span>
        )}
        <button className="trip-clear" onClick={props.onClear} aria-label={t('trip.clear')}>✕</button>
      </div>
    )
  }

  return (
    <div className="tripbar">
      <input
        className="trip-input"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder={t('trip.addDestination')}
        aria-label={t('trip.addDestination')}
      />
      {results.length > 0 && (
        <ul className="trip-results">
          {results.map((r) => (
            <li key={`${r.lat},${r.lon}`}>
              <button onClick={() => { props.onSet(r); setQ(''); setResults([]) }}>{r.label}</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
