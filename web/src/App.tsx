import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FindPage } from './FindPage'
import { API_BASE, type Charger } from './api'
import { buildPath, parseUrl, type NavState } from './url'
import { useSettings } from './settings'
import { useTheme } from './theme'
import { ProfileBar, SettingsPanel, FilterBar, type Filters } from './ui'
import { reverseGeocode, shortPlace, type Place } from './geocode'

const GITHUB_URL = 'https://github.com/jeroentrappers/charging'

// Insights pulls in the charting library; load it only when that tab is opened.
const InsightsPage = lazy(() => import('./InsightsPage').then((m) => ({ default: m.InsightsPage })))

// Default to Ghent until geolocation resolves (or is denied).
const DEFAULT_CENTER: [number, number] = [51.0543, 3.725]

export default function App() {
  const { t } = useTranslation()

  // URL-driven navigation. `route` mirrors the address bar; `routeNonce` bumps
  // only on load + back/forward (popstate), so FindPage applies the URL then but
  // ignores our own pushes (which already updated app state).
  const [route, setRoute] = useState<NavState>(() => parseUrl(window.location.pathname))
  const [routeNonce, setRouteNonce] = useState(0)
  const tab = route.tab
  useEffect(() => {
    const onPop = () => {
      setRoute(parseUrl(window.location.pathname))
      setRouteNonce((n) => n + 1)
    }
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  // navigate updates the address bar + route mirror for a user action (no
  // routeNonce bump). push=false coalesces rapid updates (map panning).
  function navigate(next: NavState, push: boolean) {
    const path = buildPath(next)
    if (path !== window.location.pathname) {
      if (push) window.history.pushState(null, '', path)
      else window.history.replaceState(null, '', path)
    }
    setRoute(next)
  }
  const setTab = (next: 'find' | 'insights') =>
    navigate({ tab: next, center: route.center }, true) // keep the map centre; drop any open charger
  const onCenter = (center: { lat: number; lon: number; zoom: number }) => navigate({ tab: 'find', center }, false)
  const onOpenCharger = (c: Charger) =>
    navigate({ tab: 'find', center: route.center, chargerId: c.id, chargerSlug: c.name || c.cpo_id }, true)
  const onCloseCharger = () => navigate({ tab: 'find', center: route.center }, true)

  const [settings, patchSettings] = useSettings()
  const [theme, setTheme] = useTheme()
  const [showSettings, setShowSettings] = useState(false)
  const [filters, setFilters] = useState<Filters>({ available: false, minPower: 0, plug: '', includePrivate: false, plugCompatible: false })
  const [filtersOpen, setFiltersOpen] = useState(false) // collapsed by default to give the list room (esp. mobile)
  const [located, setLocated] = useState<[number, number] | null>(null)
  const [accuracy, setAccuracy] = useState<number | null>(null) // GPS accuracy radius, metres
  const [geoNote, setGeoNote] = useState('')
  const [geoNonce, setGeoNonce] = useState(0) // bumps on each explicit locate -> recenter + re-follow geo
  const [locLabel, setLocLabel] = useState('') // reverse-geocoded address of the current location
  const [tripTo, setTripTo] = useState<Place | null>(null) // corridor destination (shown in the header)
  const watchId = useRef<number | null>(null)
  const bestAccuracy = useRef<number>(Infinity) // smallest accuracy radius seen this locate cycle

  // Accept a GPS reading only if it's the first fix of this cycle or strictly
  // more accurate than the best so far. This refines the location as the fix
  // converges, then stops moving it on sub-metre jitter — so the list isn't
  // re-queried on every watchPosition tick. Re-tapping "locate" re-acquires.
  function acceptFix(p: GeolocationPosition) {
    if (p.coords.accuracy >= bestAccuracy.current) return
    bestAccuracy.current = p.coords.accuracy
    setLocated([p.coords.latitude, p.coords.longitude])
    setAccuracy(p.coords.accuracy)
  }

  // Live refinement: only nudges the location when accuracy improves.
  function startWatch() {
    if (watchId.current != null || !navigator.geolocation) return
    watchId.current = navigator.geolocation.watchPosition(
      acceptFix,
      () => {},
      { enableHighAccuracy: true, maximumAge: 10000 },
    )
  }

  // Explicit locate: get a fix now, recenter the map, and start refining.
  function locate() {
    if (!navigator.geolocation) {
      setGeoNote('geo.notSupported')
      return
    }
    setGeoNote('geo.locating')
    bestAccuracy.current = Infinity // fresh cycle: re-acquire from scratch
    navigator.geolocation.getCurrentPosition(
      (p) => {
        acceptFix(p)
        setGeoNote('')
        setGeoNonce((n) => n + 1)
        startWatch()
      },
      (err) => {
        // Geolocation needs a secure context (HTTPS or localhost); a non-secure
        // origin or a denial both land here.
        setGeoNote(err.code === err.PERMISSION_DENIED ? 'geo.blocked' : 'geo.unavailable')
      },
      { enableHighAccuracy: true, timeout: 8000, maximumAge: 30000 },
    )
  }
  useEffect(() => {
    locate()
    return () => {
      if (watchId.current != null) navigator.geolocation.clearWatch(watchId.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Reverse-geocode the current location to a readable address (debounced;
  // located changes only when the fix improves, so this is low-volume).
  useEffect(() => {
    if (!located) {
      setLocLabel('')
      return
    }
    const ctrl = new AbortController()
    const h = setTimeout(() => {
      reverseGeocode(located[0], located[1], ctrl.signal).then(setLocLabel).catch(() => {})
    }, 400)
    return () => {
      clearTimeout(h)
      ctrl.abort()
    }
  }, [located?.[0], located?.[1]])

  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">
          <svg viewBox="0 0 512 512" aria-hidden><path d="M286 64 134 296h92l-40 152 192-248h-96z" fill="#15803d" /></svg>
          Charging
        </span>
        <nav className="tabs">
          <button className={tab === 'find' ? 'active' : ''} onClick={() => setTab('find')}>{t('nav.find')}</button>
          <button className={tab === 'insights' ? 'active' : ''} onClick={() => setTab('insights')}>{t('nav.insights')}</button>
        </nav>
        <div className="header-links">
          <button className="hlink hlink-btn" onClick={() => setShowSettings(true)} aria-label={t('settings.title')} title={t('settings.title')}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
              <line x1="4" y1="8" x2="20" y2="8" /><line x1="4" y1="16" x2="20" y2="16" />
              <circle cx="9" cy="8" r="2.4" fill="var(--surface)" /><circle cx="15" cy="16" r="2.4" fill="var(--surface)" />
            </svg>
          </button>
          <a className="hlink" href={`${API_BASE}/docs`} target="_blank" rel="noreferrer">{t('nav.apiDocs')}</a>
          <a className="hlink" href={GITHUB_URL} target="_blank" rel="noreferrer" aria-label="GitHub" title="GitHub">
            <svg viewBox="0 0 16 16" aria-hidden><path fillRule="evenodd" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
          </a>
        </div>
      </header>

      {tab === 'find' && (
        <div className="controls">
          <div className="filters">
            <button className="chip loc-chip" onClick={locate} title={t('geo.locate')}>
              📍 {locLabel || t('geo.locate')}
            </button>
            {tripTo && (
              <span className="chip dest-chip">
                🏁 {shortPlace(tripTo.label)}
                <button className="dest-x" onClick={() => setTripTo(null)} aria-label={t('trip.clear')}>✕</button>
              </span>
            )}
            {(() => {
              const n =
                (filters.available ? 1 : 0) +
                (filters.plugCompatible ? 1 : 0) +
                (filters.minPower ? 1 : 0) +
                (filters.plug ? 1 : 0) +
                (filters.includePrivate ? 1 : 0)
              return (
                <button
                  className={`chip ${n ? 'on' : ''}`}
                  aria-expanded={filtersOpen}
                  onClick={() => setFiltersOpen((o) => !o)}
                >
                  {t('filters.title')}{n ? ` · ${n}` : ''} {filtersOpen ? '▾' : '▸'}
                </button>
              )
            })()}
          </div>
          {/* Energy + Speed (ProfileBar) and the filter chips fold away by
              default to keep the header small; the Filters toggle reveals them. */}
          {filtersOpen && (
            <div className="filter-panel">
              <ProfileBar car={settings.car} charge={settings.charge} onCharge={(charge) => patchSettings({ charge })} />
              <FilterBarInline filters={filters} setFilters={setFilters} carPlugs={settings.car.plugs} />
            </div>
          )}
          {geoNote && <div className="geo-note">{t(geoNote)}</div>}
        </div>
      )}

      {tab === 'find' ? (
        <FindPage
          fallbackCenter={located ?? DEFAULT_CENTER}
          located={located}
          accuracy={accuracy}
          geoNonce={geoNonce}
          route={route}
          routeNonce={routeNonce}
          onCenter={onCenter}
          onOpenCharger={onOpenCharger}
          onCloseCharger={onCloseCharger}
          settings={settings}
          filters={filters}
          tripTo={tripTo}
          onSetTrip={setTripTo}
          onClearTrip={() => setTripTo(null)}
        />
      ) : (
        <Suspense fallback={<div className="insights"><div className="state"><div className="spinner" />{t('insights.loading')}</div></div>}>
          <InsightsPage />
        </Suspense>
      )}

      <nav className="bottomnav">
        <button className={tab === 'find' ? 'active' : ''} onClick={() => setTab('find')}>{t('nav.find')}</button>
        <button className={tab === 'insights' ? 'active' : ''} onClick={() => setTab('insights')}>{t('nav.insights')}</button>
      </nav>

      {showSettings && (
        <SettingsPanel
          settings={settings}
          onChange={patchSettings}
          theme={theme}
          onTheme={setTheme}
          onClose={() => setShowSettings(false)}
        />
      )}
    </div>
  )
}

// FilterBar renders its own .filters row; here we only want the chips, so reuse
// it directly (the extra wrapper above hosts the Locate button alongside).
function FilterBarInline({ filters, setFilters, carPlugs }: { filters: Filters; setFilters: (f: Filters) => void; carPlugs?: string[] }) {
  return <FilterBar f={filters} onChange={setFilters} carPlugs={carPlugs} />
}
