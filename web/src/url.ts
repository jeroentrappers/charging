// Beautiful, shareable URLs that mirror in-app navigation. No router dependency
// — just the History API. Schemes:
//   /                          find, default view
//   /@51.05432,3.72500,13z     find, map centred here at zoom 13
//   /charger/3019/markt-gent   find, charger 3019 detail open (slug is decorative)
//   /insights                  insights tab

export interface NavState {
  tab: 'find' | 'insights'
  center?: { lat: number; lon: number; zoom: number }
  chargerId?: number
  chargerSlug?: string
}

// slugify makes a short, readable, URL-safe label from a charger name.
export function slugify(s: string): string {
  return s
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '') // strip accents
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
    .replace(/-+$/g, '')
}

const atRe = /^\/@(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?),(\d+(?:\.\d+)?)z?$/
const chargerRe = /^\/charger\/(\d+)(?:\/([^/]*))?$/

export function parseUrl(pathname: string): NavState {
  let p = pathname
  try {
    p = decodeURIComponent(pathname)
  } catch {
    /* keep raw */
  }
  if (p === '/insights') return { tab: 'insights' }

  const cm = chargerRe.exec(p)
  if (cm) return { tab: 'find', chargerId: Number(cm[1]), chargerSlug: cm[2] || undefined }

  const am = atRe.exec(p)
  if (am) {
    return { tab: 'find', center: { lat: Number(am[1]), lon: Number(am[2]), zoom: Number(am[3]) } }
  }
  return { tab: 'find' }
}

export function buildPath(s: NavState): string {
  if (s.tab === 'insights') return '/insights'
  if (s.chargerId != null) {
    const slug = s.chargerSlug ? slugify(s.chargerSlug) : ''
    return slug ? `/charger/${s.chargerId}/${slug}` : `/charger/${s.chargerId}`
  }
  if (s.center) {
    return `/@${s.center.lat.toFixed(5)},${s.center.lon.toFixed(5)},${Math.round(s.center.zoom)}z`
  }
  return '/'
}
