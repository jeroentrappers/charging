// Self-hosted vector basemap on tiles.appmire.be (shared appmire infra: PMTiles
// + MapLibre styles, gated by a coarse ?key= that ships in the client). When no
// key is configured we fall back to raster OpenStreetMap, so dev still works.
//
// Resolution order mirrors api.ts: runtime /config.js (window.__CONFIG__) first
// — the production path — then build-time VITE_ vars.
const rc = typeof window !== 'undefined' ? window.__CONFIG__ : undefined

const trimSlash = (s: string) => s.replace(/\/$/, '')

export const TILES_URL = trimSlash(
  rc?.tilesUrl || import.meta.env.VITE_TILES_URL || 'https://tiles.appmire.be',
)
export const TILES_KEY = rc?.tilesKey || import.meta.env.VITE_TILES_KEY || ''

// Whether the self-hosted vector basemap is available (a key is configured).
export const hasVectorTiles = TILES_KEY !== ''

// MapLibre style URL for the given theme. The style JSON embeds the key in its
// own sprite/glyph/pmtiles URLs, so we only carry it on the initial fetch.
export function styleUrl(dark: boolean): string {
  const name = dark ? 'dark' : 'liberty'
  return `${TILES_URL}/styles/${name}.json?key=${encodeURIComponent(TILES_KEY)}`
}
