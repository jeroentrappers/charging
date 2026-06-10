import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FindPage } from './FindPage'
import { LANGS } from './i18n'
import { SessionBar, FilterBar, sessionKey, type Filters, type CustomSession } from './ui'

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
