import { useEffect, useMemo, useRef } from 'react'
import type { MutableRefObject } from 'react'
import { MapContainer, TileLayer, CircleMarker, Circle, useMap, useMapEvents } from 'react-leaflet'
import type { Charger } from './api'
import { priceColor, priceOf } from './ui'

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

// Flies to the selected charger so it's centred and visible.
function FocusOn({ to, nonce }: { to: [number, number] | null; nonce: number }) {
  const map = useMap()
  const last = useRef(-1)
  useEffect(() => {
    if (!to || nonce === last.current) return
    last.current = nonce
    map.flyTo(to, Math.max(map.getZoom(), 15), { duration: 0.4 })
  }, [to, nonce, map])
  return null
}

// Reports viewport center + radius (m) after the map settles, plus once on load.
function Viewport({ onChange }: { onChange: (lat: number, lon: number, radiusM: number) => void }) {
  const map = useMapEvents({ moveend: () => emit() })
  function emit() {
    const c = map.getCenter()
    const r = c.distanceTo(map.getBounds().getNorthEast())
    onChange(c.lat, c.lng, Math.min(Math.round(r), 50000))
  }
  useEffect(() => {
    emit()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
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
  recenterTo: [number, number] | null
  recenterNonce: number
  focus: [number, number] | null
  focusNonce: number
  origin: [number, number] | null
  showOrigin: boolean
  accuracyM: number | null
  chargers: Charger[]
  selectedId: number | null
  onSelect: (id: number) => void
  onViewport: (lat: number, lon: number, radiusM: number) => void
  onPick: (lat: number, lon: number) => void
}) {
  const markerClick = useRef(0)
  const [min, max] = useMemo(() => {
    const ps = props.chargers.map(priceOf).filter((p): p is number => p != null)
    return ps.length ? [Math.min(...ps), Math.max(...ps)] : [0, 0]
  }, [props.chargers])

  return (
    <div className="map">
      <MapContainer center={props.initial} zoom={13} zoomControl={false} style={{ height: '100%' }}>
        <TileLayer attribution="&copy; OpenStreetMap" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
        <AutoResize />
        <RecenterOnNonce to={props.recenterTo} nonce={props.recenterNonce} />
        <FocusOn to={props.focus} nonce={props.focusNonce} />
        <Viewport onChange={props.onViewport} />
        <MapClicker onPick={props.onPick} markerClick={markerClick} />

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
            <CircleMarker
              key={c.id}
              center={[c.lat, c.lon]}
              radius={sel ? 12 : 8}
              pathOptions={{
                color: sel ? '#0f172a' : '#ffffff',
                weight: sel ? 3 : 1.5,
                fillColor: priceColor(priceOf(c), min, max),
                fillOpacity: c.availability_stale ? 0.45 : 0.95,
              }}
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
