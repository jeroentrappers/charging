import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FindPage } from './FindPage'
import { API_BASE, track, type Charger } from './api'
import { buildPath, parseUrl, type NavState } from './url'
import { useSettings } from './settings'
import { useTheme } from './theme'
import { ProfileBar, SettingsPanel, FilterBar, type Filters } from './ui'
import { geocode, reverseGeocode, shortPlace, type Place } from './geocode'

const GITHUB_URL = 'https://github.com/jeroentrappers/charging'

// Insights pulls in the charting library; load it only when that tab is opened.
const InsightsPage = lazy(() => import('./InsightsPage').then((m) => ({ default: m.InsightsPage })))
const StatusPage = lazy(() => import('./StatusPage').then((m) => ({ default: m.StatusPage })))
const ChargersPage = lazy(() => import('./ChargersPage').then((m) => ({ default: m.ChargersPage })))
const AdminAnalyticsPage = lazy(() => import('./AdminAnalyticsPage').then((m) => ({ default: m.AdminAnalyticsPage })))

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

  // First-party analytics: app open (once per load) + PWA install.
  useEffect(() => {
    track('app_open', { launch: window.matchMedia('(display-mode: standalone)').matches ? 'pwa' : 'browser' })
    const onInstalled = () => track('pwa_install')
    window.addEventListener('appinstalled', onInstalled)
    return () => window.removeEventListener('appinstalled', onInstalled)
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
  const setTab = (next: 'find' | 'insights' | 'status' | 'chargers') =>
    navigate({ tab: next, center: route.center }, true) // keep the map centre; drop any open charger
  const onCenter = (center: { lat: number; lon: number; zoom: number }) => navigate({ tab: 'find', center }, false)
  const onOpenCharger = (c: Charger) =>
    navigate({ tab: 'find', center: route.center, chargerId: c.id, chargerSlug: c.name || c.cpo_id }, true)
  const onCloseCharger = () => navigate({ tab: 'find', center: route.center }, true)

  const [settings, patchSettings] = useSettings()
  // Debounced session-profile change event (the price slider): one event per
  // settled adjustment, and never on first render.
  const chargeInit = useRef(true)
  useEffect(() => {
    if (chargeInit.current) {
      chargeInit.current = false
      return
    }
    const id = window.setTimeout(
      () => track('session_change', { kwh: settings.charge.kWh, power: settings.charge.powerKW }),
      1000,
    )
    return () => window.clearTimeout(id)
  }, [settings.charge.kWh, settings.charge.powerKW])
  const [theme, setTheme] = useTheme()
  const [showSettings, setShowSettings] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false) // mobile fly-out nav under the brand
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

  // Esc closes the fly-out nav.
  useEffect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [menuOpen])

  return (
    <div className="app">
      <header className="topbar">
        <button
          className="brand brand-btn"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((o) => !o)}
        >
          <svg viewBox="0 0 512 512" aria-hidden><path d="M286 64 134 296h92l-40 152 192-248h-96z" fill="#15803d" /></svg>
          Charging
          <span className="brand-caret" aria-hidden>▾</span>
        </button>
        {menuOpen && (
          <>
            <div className="nav-backdrop" onClick={() => setMenuOpen(false)} />
            <div className="navmenu" role="menu">
              <button role="menuitem" className={tab === 'find' ? 'active' : ''} onClick={() => { setTab('find'); setMenuOpen(false) }}>{t('nav.find')}</button>
              <button role="menuitem" className={tab === 'chargers' ? 'active' : ''} onClick={() => { setTab('chargers'); setMenuOpen(false) }}>{t('nav.chargers')}</button>
              <button role="menuitem" className={tab === 'insights' ? 'active' : ''} onClick={() => { setTab('insights'); setMenuOpen(false) }}>{t('nav.insights')}</button>
              <button role="menuitem" className={tab === 'status' ? 'active' : ''} onClick={() => { setTab('status'); setMenuOpen(false) }}>{t('nav.status')}</button>
              <button role="menuitem" onClick={() => { setShowSettings(true); setMenuOpen(false) }}>{t('settings.title')}</button>
              <div className="navmenu-sep" />
              <a role="menuitem" href={`${API_BASE}/docs`} target="_blank" rel="noreferrer" onClick={() => setMenuOpen(false)}>{t('nav.apiDocs')}</a>
              <a role="menuitem" href={GITHUB_URL} target="_blank" rel="noreferrer" onClick={() => setMenuOpen(false)}>GitHub</a>
            </div>
          </>
        )}
        {/* The find controls share the top row with the brand/menu button. The
            navigation, settings and links all live in the fly-out menu now (on
            every screen size), so the bar stays a single compact row. */}
        {tab === 'find' && (
          <div className="topctrls">
            <button className="chip loc-chip" onClick={locate} title={t('geo.locate')}>
              📍 {locLabel || t('geo.locate')}
            </button>
            {tripTo ? (
              <span className="chip dest-chip">
                🏁 {shortPlace(tripTo.label)}
                <button className="dest-x" onClick={() => setTripTo(null)} aria-label={t('trip.clear')}>✕</button>
              </span>
            ) : (
              <DestinationSearch onSet={setTripTo} />
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
        )}
      </header>

      {/* Energy + Speed (ProfileBar) and the filter chips fold away by default
          to keep the header small; the Filters toggle in the top row reveals
          them. The geo note also drops below the bar. */}
      {tab === 'find' && (filtersOpen || geoNote) && (
        <div className="controls">
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
      ) : tab === 'insights' ? (
        <Suspense fallback={<div className="insights"><div className="state"><div className="spinner" />{t('insights.loading')}</div></div>}>
          <InsightsPage />
        </Suspense>
      ) : tab === 'chargers' ? (
        <Suspense fallback={<div className="chargers-page"><div className="state"><div className="spinner" />{t('chargers.loading')}</div></div>}>
          <ChargersPage />
        </Suspense>
      ) : tab === 'admin' ? (
        <Suspense fallback={<div className="admin-page"><div className="state"><div className="spinner" />…</div></div>}>
          <AdminAnalyticsPage />
        </Suspense>
      ) : (
        <Suspense fallback={<div className="status-page"><div className="state"><div className="spinner" />{t('status.loading')}</div></div>}>
          <StatusPage />
        </Suspense>
      )}

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

// Destination search lives in the header now (next to the location chip), not in
// the list sheet. Debounced Nominatim lookup; picking a result sets the trip.
function DestinationSearch({ onSet }: { onSet: (p: Place) => void }) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Place[]>([])
  useEffect(() => {
    if (q.trim().length < 3) {
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
  }, [q])
  return (
    <div className="dest-search">
      <input
        className="dest-input"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder={t('trip.addDestination')}
        aria-label={t('trip.addDestination')}
      />
      {results.length > 0 && (
        <ul className="trip-results">
          {results.map((r) => (
            <li key={`${r.lat},${r.lon}`}>
              <button
                onClick={() => {
                  onSet(r)
                  setQ('')
                  setResults([])
                }}
              >
                {r.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
