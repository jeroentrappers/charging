import { useEffect, useMemo, useRef } from 'react'
import { MapContainer, TileLayer, CircleMarker, useMap, useMapEvents } from 'react-leaflet'
import type { Charger } from './api'
import { priceColor, priceOf } from './ui'

// Reports viewport center + radius (m) after the map settles, plus once on load.
function Viewport({ onChange }: { onChange: (lat: number, lon: number, radiusM: number) => void }) {
  const map = useMapEvents({
    moveend: () => emit(),
  })
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

// Recenters the map when `to` changes (e.g. after geolocation), without fighting
// the user's manual panning.
function Recenter({ to }: { to: [number, number] | null }) {
  const map = useMap()
  const last = useRef<string>('')
  useEffect(() => {
    if (!to) return
    const key = to.join(',')
    if (key !== last.current) {
      last.current = key
      map.setView(to, Math.max(map.getZoom(), 13))
    }
  }, [to, map])
  return null
}

export function MapView(props: {
  initial: [number, number]
  recenterTo: [number, number] | null
  chargers: Charger[]
  selectedId: number | null
  onSelect: (id: number) => void
  onViewport: (lat: number, lon: number, radiusM: number) => void
}) {
  const [min, max] = useMemo(() => {
    const ps = props.chargers.map(priceOf).filter((p): p is number => p != null)
    return ps.length ? [Math.min(...ps), Math.max(...ps)] : [0, 0]
  }, [props.chargers])

  return (
    <div className="map">
      <MapContainer center={props.initial} zoom={13} zoomControl={false} style={{ height: '100%' }}>
        <TileLayer
          attribution='&copy; OpenStreetMap'
          url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
        />
        <Recenter to={props.recenterTo} />
        <Viewport onChange={props.onViewport} />
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
              eventHandlers={{ click: () => props.onSelect(c.id) }}
            />
          )
        })}
      </MapContainer>
    </div>
  )
}
