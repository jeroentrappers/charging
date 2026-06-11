import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Charger } from './api'
import { MapView } from './MapView'
import type { NavState } from './url'
import { AvailBadge, availOf, eur, km, priceOf, type Filters } from './ui'

const ChargerDetail = lazy(() => import('./ChargerDetail').then((m) => ({ default: m.ChargerDetail })))

interface Viewport { lat: number; lon: number; radius: number }

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
  sessionKey: string | undefined
  energyKWh?: number // custom session: energy to add (overrides sessionKey)
  powerKW?: number // custom session: power cap; undefined = as fast as possible
  filters: Filters
}) {
  const { t } = useTranslation()
  const [vp, setVp] = useState<Viewport | null>(null)
  const [manualOrigin, setManualOrigin] = useState<[number, number] | null>(null)
  const [chargers, setChargers] = useState<Charger[]>([])
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
  const radius = vp?.radius ?? 5000

  // Latest values for the (nonce-keyed) URL-apply effect, without making them deps.
  const chargersRef = useRef(chargers)
  chargersRef.current = chargers
  const originRef = useRef(origin)
  originRef.current = origin

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
          energy_kwh: props.energyKWh,
          power_kw: props.powerKW,
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
  }, [oLat, oLon, radius, props.sessionKey, props.energyKWh, props.powerKW, props.filters])

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
    const inList = chargersRef.current.find((c) => c.id === id)
    if (inList) {
      setDetailCharger(inList)
      setFocusNonce((n) => n + 1)
    } else {
      api
        .charger(id, originRef.current[0], originRef.current[1])
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

  function onViewport(lat: number, lon: number, r: number, zoom: number) {
    setVp({ lat, lon, radius: r })
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
      />

      {!hasFix && <div className="map-hint">{t('find.tapHint')}</div>}

      <div className={`sheet ${expanded ? 'expanded' : ''}`}>
        <div className="handle"><button aria-label="toggle list" onClick={() => setExpanded((e) => !e)} /></div>
        <div className="sheet-head">
          <h2>{loading ? t('find.searching') : t('find.chargers', { count: chargers.length })}</h2>
          <span className="muted">{t('find.cheapestFirst')}</span>
        </div>
        <div className="list">
          {error && <div className="state">{t('find.loadError')}</div>}
          {!error && !loading && chargers.length === 0 && (
            <div className="state">{props.filters.available ? t('find.emptyAvailable') : t('find.empty')}</div>
          )}
          {chargers.map((c) => (
            <button key={c.id} className={`row ${c.id === selectedId ? 'sel' : ''}`} onClick={() => select(c.id)}>
              <span className="price">{eur(priceOf(c))}</span>
              <span>
                <div className="name">{c.name || c.cpo_id}</div>
                <div className="sub">
                  {c.power_kw} kW {c.current_type} · {km(c.distance_m)}
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
