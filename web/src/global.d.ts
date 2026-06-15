export {}

// maplibre-gl-leaflet ships no types; it augments Leaflet's L with maplibreGL().
declare module '@maplibre/maplibre-gl-leaflet'

declare global {
  interface Window {
    // Injected at runtime by /config.js (generated from env on container start).
    __CONFIG__?: { apiBase?: string; tilesUrl?: string; tilesKey?: string }
  }
}
