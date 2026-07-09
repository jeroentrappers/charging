import { useEffect, useMemo, useRef, useState } from 'react'
import type { MutableRefObject } from 'react'
import { MapContainer, TileLayer, CircleMarker, Circle, Marker, Polyline, useMap, useMapEvents } from 'react-leaflet'
import L from 'leaflet'
import { track, type Charger } from './api'
import { priceColor, priceOf } from './ui'
import { hasVectorTiles, styleUrl } from './tiles'

// MapLibre (heavy, ~300 kB) is loaded on demand the first time a vector basemap
// is shown: register the pmtiles protocol (the style's openmaptiles source is
// pmtiles://…) and expose maplibregl to the maplibre-gl-leaflet binding, once.
let glReady: Promise<void> | null = null
function loadGL(): Promise<void> {
  if (!glReady) {
    glReady = (async () => {
      const [maplibre, pm] = await Promise.all([import('maplibre-gl'), import('pmtiles')])
      await import('maplibre-gl/dist/maplibre-gl.css')
      await import('@maplibre/maplibre-gl-leaflet')
      const maplibregl = maplibre.default
      maplibregl.addProtocol('pmtiles', new pm.Protocol().tile)
      ;(window as unknown as { maplibregl?: unknown }).maplibregl = maplibregl
    })()
  }
  return glReady
}

// Basemap: the self-hosted vector tiles (tiles.appmire.be, theme-matched) when a
// key is configured, rendered as a MapLibre GL layer *under* the Leaflet markers
// so the price pins / route / origin overlays are untouched. Falls back to raster
// OpenStreetMap otherwise (dev, or no key).
// Track the effective theme (reflected on <html data-theme>) so the vector
// basemap can swap between the light/dark styles when the user toggles it.
function useDarkTheme(): boolean {
  const [dark, setDark] = useState(() =>
    typeof document !== 'undefined' && document.documentElement.dataset.theme === 'dark',
  )
  useEffect(() => {
    const el = document.documentElement
    const obs = new MutationObserver(() => setDark(el.dataset.theme === 'dark'))
    obs.observe(el, { attributes: true, attributeFilter: ['data-theme'] })
    return () => obs.disconnect()
  }, [])
  return dark
}

function Basemap({ dark }: { dark: boolean }) {
  const map = useMap()
  useEffect(() => {
    if (!hasVectorTiles) return
    let layer: L.Layer | null = null
    let cancelled = false
    loadGL().then(() => {
      if (cancelled) return
      layer = (L as unknown as { maplibreGL: (o: { style: string }) => L.Layer }).maplibreGL({
        style: styleUrl(dark),
      })
      layer.addTo(map)
    })
    return () => {
      cancelled = true
      if (layer) map.removeLayer(layer)
    }
  }, [map, dark])
  if (hasVectorTiles) return null
  return <TileLayer attribution="&copy; OpenStreetMap" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
}

// A color-coded price pill anchored on the charger's coordinate. Showing the
// price right on the map (not just a dot) makes the cheapest options pop before
// the user even reads the list.
function pricePin(c: Charger, color: string, sel: boolean): L.DivIcon {
  const p = priceOf(c)
  const label = p == null ? '–' : `€${Math.round(p)}`
  const cls = ['price-pin', sel && 'sel', c.avoid && 'avoid', c.availability_stale && 'stale'].filter(Boolean).join(' ')
  return L.divIcon({
    className: 'price-pin-icon',
    html: `<span class="${cls}" style="background:${color}">${label}</span>`,
    iconSize: [0, 0],
  })
}

// Keeps Leaflet's internal size in sync when the map container resizes (the
// mobile split, orientation changes) — otherwise tiles grey out.
function AutoResize() {
  const map = useMap()
  useEffect(() => {
    const el = map.getContainer()
    const ro = new ResizeObserver(() => map.invalidateSize())
    ro.observe(el)
    return () => ro.disconnect()
  }, [map])
  return null
}

// Recenters when `nonce` changes (an explicit "Locate me"), so live geolocation
// updates move the pin without yanking the map while the user is panning.
function RecenterOnNonce({ to, nonce }: { to: [number, number] | null; nonce: number }) {
  const map = useMap()
  const last = useRef(-1)
  useEffect(() => {
    if (!to || nonce === last.current) return
    last.current = nonce
    map.setView(to, Math.max(map.getZoom(), 14))
  }, [to, nonce, map])
  return null
}

// Pans to the selected charger so it's centred and visible. Keeps the current
// zoom on purpose: zooming in would shrink the search radius, which re-runs the
// query narrower and can drop the very charger that was selected.
function FocusOn({ to, nonce }: { to: [number, number] | null; nonce: number }) {
  const map = useMap()
  const last = useRef(-1)
  useEffect(() => {
    if (!to || nonce === last.current) return
    last.current = nonce
    map.panTo(to, { duration: 0.4 })
  }, [to, nonce, map])
  return null
}

// Reports viewport center + radius (m) + zoom after the map settles, plus once
// on load.
function Viewport({ onChange }: { onChange: (lat: number, lon: number, radiusM: number, zoom: number) => void }) {
  const trackTimer = useRef<number | undefined>(undefined)
  const map = useMapEvents({
    moveend: () => {
      emit()
      // Debounce so a pan/zoom gesture records one map_move, not a burst.
      window.clearTimeout(trackTimer.current)
      trackTimer.current = window.setTimeout(() => track('map_move', { zoom: map.getZoom() }), 1200)
    },
  })
  function emit() {
    const c = map.getCenter()
    const r = c.distanceTo(map.getBounds().getNorthEast())
    onChange(c.lat, c.lng, Math.min(Math.round(r), 50000), map.getZoom())
  }
  useEffect(() => {
    emit()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return null
}

// Sets the view (center + zoom) when `nonce` changes — used to apply a URL/route
// (deep link, back/forward) to the map.
function SetViewOnNonce({ to, zoom, nonce }: { to: [number, number] | null; zoom?: number; nonce: number }) {
  const map = useMap()
  const last = useRef(-1)
  useEffect(() => {
    if (!to || nonce === last.current) return
    last.current = nonce
    map.setView(to, zoom ?? map.getZoom())
  }, [to, zoom, nonce, map])
  return null
}

// Fits the map to the whole trip route when it (re)loads.
function FitRoute({ route, nonce }: { route: [number, number][] | null; nonce: number }) {
  const map = useMap()
  const last = useRef(-1)
  useEffect(() => {
    if (!route || route.length < 2 || nonce === last.current) return
    last.current = nonce
    map.fitBounds(route as L.LatLngBoundsLiteral, { padding: [40, 40] })
  }, [route, nonce, map])
  return null
}

// A click on the map background (not on a charger) drops the origin pin.
function MapClicker({ onPick, markerClick }: { onPick: (lat: number, lon: number) => void; markerClick: MutableRefObject<number> }) {
  useMapEvents({
    click(e) {
      // Ignore the map click that fires right after a charger marker click.
      if (Date.now() - markerClick.current < 150) return
      onPick(e.latlng.lat, e.latlng.lng)
    },
  })
  return null
}

export function MapView(props: {
  initial: [number, number]
  initialZoom?: number
  recenterTo: [number, number] | null
  recenterNonce: number
  focus: [number, number] | null
  focusNonce: number
  viewTo: [number, number] | null
  viewZoom?: number
  viewNonce: number
  origin: [number, number] | null
  showOrigin: boolean
  accuracyM: number | null
  chargers: Charger[]
  selectedId: number | null
  onSelect: (id: number) => void
  onViewport: (lat: number, lon: number, radiusM: number, zoom: number) => void
  onPick: (lat: number, lon: number) => void
  route?: [number, number][] | null // trip route polyline (lat,lon)
  dest?: [number, number] | null // trip destination
  routeNonce?: number // bumps when a new route loads, to fit bounds
}) {
  const markerClick = useRef(0)
  const dark = useDarkTheme()
  const [min, max] = useMemo(() => {
    const ps = props.chargers.map(priceOf).filter((p): p is number => p != null)
    return ps.length ? [Math.min(...ps), Math.max(...ps)] : [0, 0]
  }, [props.chargers])

  return (
    <div className="map">
      <MapContainer center={props.initial} zoom={props.initialZoom ?? 13} zoomControl={false} style={{ height: '100%' }}>
        <Basemap dark={dark} />
        <AutoResize />
        <RecenterOnNonce to={props.recenterTo} nonce={props.recenterNonce} />
        <SetViewOnNonce to={props.viewTo} zoom={props.viewZoom} nonce={props.viewNonce} />
        <FocusOn to={props.focus} nonce={props.focusNonce} />
        <Viewport onChange={props.onViewport} />
        <MapClicker onPick={props.onPick} markerClick={markerClick} />
        <FitRoute route={props.route ?? null} nonce={props.routeNonce ?? 0} />

        {/* Trip route + destination */}
        {props.route && props.route.length > 1 && (
          <Polyline positions={props.route} pathOptions={{ color: '#2563eb', weight: 5, opacity: 0.65 }} />
        )}
        {props.dest && (
          <Marker
            position={props.dest}
            icon={L.divIcon({ className: 'price-pin-icon', html: '<span class="dest-pin">🏁</span>', iconSize: [0, 0] })}
            zIndexOffset={500}
          />
        )}

        {/* Origin ("you are here" / chosen point) — anchored to coordinates, so
            it pans with the map. Distances are measured from here. */}
        {props.showOrigin && props.origin && (
          <>
            {props.accuracyM != null && props.accuracyM > 0 ? (
              // Real-world GPS accuracy radius (metres) — scales with zoom.
              <Circle center={props.origin} radius={props.accuracyM} pathOptions={{ color: '#2563eb', weight: 1, opacity: 0.4, fillColor: '#2563eb', fillOpacity: 0.12 }} />
            ) : (
              <CircleMarker center={props.origin} radius={18} pathOptions={{ stroke: false, fillColor: '#2563eb', fillOpacity: 0.15 }} />
            )}
            <CircleMarker center={props.origin} radius={8} pathOptions={{ color: '#fff', weight: 3, fillColor: '#2563eb', fillOpacity: 1 }} />
          </>
        )}

        {props.chargers.map((c) => {
          const sel = c.id === props.selectedId
          return (
            <Marker
              key={c.id}
              position={[c.lat, c.lon]}
              icon={pricePin(c, priceColor(priceOf(c), min, max), sel)}
              zIndexOffset={sel ? 1000 : 0}
              eventHandlers={{
                click: () => {
                  markerClick.current = Date.now()
                  props.onSelect(c.id)
                },
              }}
            />
          )
        })}
      </MapContainer>
    </div>
  )
}
