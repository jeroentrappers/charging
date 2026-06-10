import { lazy, Suspense, useEffect, useState } from 'react'
import { FindPage } from './FindPage'
import { SessionBar, FilterBar, sessionKey, type Filters } from './ui'

// Insights pulls in the charting library; load it only when that tab is opened.
const InsightsPage = lazy(() => import('./InsightsPage').then((m) => ({ default: m.InsightsPage })))

// Default to Ghent until geolocation resolves (or is denied).
const DEFAULT_CENTER: [number, number] = [51.0543, 3.725]

export default function App() {
  const [tab, setTab] = useState<'find' | 'insights'>('find')
  const [need, setNeed] = useState('best')
  const [speed, setSpeed] = useState('dc150')
  const [filters, setFilters] = useState<Filters>({ available: false, minPower: 0, plug: '' })
  const [located, setLocated] = useState<[number, number] | null>(null)

  function locate() {
    if (!navigator.geolocation) return
    navigator.geolocation.getCurrentPosition(
      (p) => setLocated([p.coords.latitude, p.coords.longitude]),
      () => {},
      { enableHighAccuracy: true, timeout: 8000 },
    )
  }
  useEffect(() => {
    locate()
  }, [])

  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">
          <svg viewBox="0 0 512 512" aria-hidden><path d="M286 64 134 296h92l-40 152 192-248h-96z" fill="#15803d" /></svg>
          Charging
        </span>
        <nav className="tabs">
          <button className={tab === 'find' ? 'active' : ''} onClick={() => setTab('find')}>Find</button>
          <button className={tab === 'insights' ? 'active' : ''} onClick={() => setTab('insights')}>Insights</button>
        </nav>
      </header>

      {tab === 'find' && (
        <div className="controls">
          <SessionBar need={need} speed={speed} onNeed={setNeed} onSpeed={setSpeed} />
          <div className="filters">
            <button className="chip" onClick={locate}>📍 Locate me</button>
            <FilterBarInline filters={filters} setFilters={setFilters} />
          </div>
        </div>
      )}

      {tab === 'find' ? (
        <FindPage
          initial={located ?? DEFAULT_CENTER}
          recenterTo={located}
          sessionKey={sessionKey(need, speed)}
          filters={filters}
        />
      ) : (
        <Suspense fallback={<div className="insights"><div className="state"><div className="spinner" />loading…</div></div>}>
          <InsightsPage />
        </Suspense>
      )}

      <nav className="bottomnav">
        <button className={tab === 'find' ? 'active' : ''} onClick={() => setTab('find')}>Find</button>
        <button className={tab === 'insights' ? 'active' : ''} onClick={() => setTab('insights')}>Insights</button>
      </nav>
    </div>
  )
}

// FilterBar renders its own .filters row; here we only want the chips, so reuse
// it directly (the extra wrapper above hosts the Locate button alongside).
function FilterBarInline({ filters, setFilters }: { filters: Filters; setFilters: (f: Filters) => void }) {
  return <FilterBar f={filters} onChange={setFilters} />
}
