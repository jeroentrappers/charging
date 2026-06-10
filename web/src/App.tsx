import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FindPage } from './FindPage'
import { API_BASE } from './api'
import { LANGS } from './i18n'
import { SessionBar, FilterBar, sessionKey, type Filters, type CustomSession } from './ui'

const GITHUB_URL = 'https://github.com/jeroentrappers/charging'

// Insights pulls in the charting library; load it only when that tab is opened.
const InsightsPage = lazy(() => import('./InsightsPage').then((m) => ({ default: m.InsightsPage })))

// Default to Ghent until geolocation resolves (or is denied).
const DEFAULT_CENTER: [number, number] = [51.0543, 3.725]

export default function App() {
  const { t, i18n } = useTranslation()
  const [tab, setTab] = useState<'find' | 'insights'>('find')
  const [need, setNeed] = useState('best')
  const [speed, setSpeed] = useState('dc150')
  const [custom, setCustom] = useState<CustomSession>({ kWh: 50, powerKW: null })
  const [filters, setFilters] = useState<Filters>({ available: false, minPower: 0, plug: '' })
  const [located, setLocated] = useState<[number, number] | null>(null)
  const [accuracy, setAccuracy] = useState<number | null>(null) // GPS accuracy radius, metres
  const [geoNote, setGeoNote] = useState('')
  const [geoNonce, setGeoNonce] = useState(0) // bumps on each explicit locate -> recenter + re-follow geo
  const watchId = useRef<number | null>(null)

  // Live updates while the user moves; doesn't recenter the map (the pin moves).
  function startWatch() {
    if (watchId.current != null || !navigator.geolocation) return
    watchId.current = navigator.geolocation.watchPosition(
      (p) => {
        setLocated([p.coords.latitude, p.coords.longitude])
        setAccuracy(p.coords.accuracy)
      },
      () => {},
      { enableHighAccuracy: true, maximumAge: 10000 },
    )
  }

  // Explicit locate: get a fix now, recenter the map, and start following.
  function locate() {
    if (!navigator.geolocation) {
      setGeoNote('geo.notSupported')
      return
    }
    setGeoNote('geo.locating')
    navigator.geolocation.getCurrentPosition(
      (p) => {
        setLocated([p.coords.latitude, p.coords.longitude])
        setAccuracy(p.coords.accuracy)
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
        <label className="lang" aria-label={t('lang.label')}>
          <select value={i18n.resolvedLanguage} onChange={(e) => i18n.changeLanguage(e.target.value)}>
            {LANGS.map((l) => (
              <option key={l.code} value={l.code}>{l.label}</option>
            ))}
          </select>
        </label>
        <div className="header-links">
          <a className="hlink" href={`${API_BASE}/docs`} target="_blank" rel="noreferrer">{t('nav.apiDocs')}</a>
          <a className="hlink" href={GITHUB_URL} target="_blank" rel="noreferrer" aria-label="GitHub" title="GitHub">
            <svg viewBox="0 0 16 16" aria-hidden><path fillRule="evenodd" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
          </a>
        </div>
      </header>

      {tab === 'find' && (
        <div className="controls">
          <SessionBar need={need} speed={speed} onNeed={setNeed} onSpeed={setSpeed} custom={custom} onCustom={setCustom} />
          <div className="filters">
            <button className="chip" onClick={locate}>📍 {t('geo.locate')}</button>
            <FilterBarInline filters={filters} setFilters={setFilters} />
          </div>
          {geoNote && <div className="geo-note">{t(geoNote)}</div>}
        </div>
      )}

      {tab === 'find' ? (
        <FindPage
          initial={located ?? DEFAULT_CENTER}
          located={located}
          accuracy={accuracy}
          geoNonce={geoNonce}
          sessionKey={need === 'custom' ? undefined : sessionKey(need, speed)}
          energyKWh={need === 'custom' ? custom.kWh : undefined}
          powerKW={need === 'custom' ? custom.powerKW ?? undefined : undefined}
          filters={filters}
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
    </div>
  )
}

// FilterBar renders its own .filters row; here we only want the chips, so reuse
// it directly (the extra wrapper above hosts the Locate button alongside).
function FilterBarInline({ filters, setFilters }: { filters: Filters; setFilters: (f: Filters) => void }) {
  return <FilterBar f={filters} onChange={setFilters} />
}
