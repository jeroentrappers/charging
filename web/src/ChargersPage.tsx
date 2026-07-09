import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ChargerListParams, type ChargerListRow, type SourceHealth, type StatusResponse } from './api'
import { eur, plugLabel, sourceConfidence } from './ui'

type SortKey = 'id' | 'name' | 'city' | 'power' | 'plug' | 'current' | 'price' | 'available' | 'source' | 'updated'
type PageSize = 25 | 50 | 100

// DESC-first mirrors the source-health page: bigger/fresher opens descending.
// "available" is treated as numeric (free connectors first when desc).
const DESC_FIRST = new Set<SortKey>(['power', 'price', 'available', 'updated'])

// Plugs the explorer exposes as filter facets. Mirrors the values stored in
// charger.plug_type (OCPI standard, normalized in ingest via model.NormalizePlug).
const PLUGS = [
  { v: '', key: 'filters.anyPlug' },
  { v: 'IEC_62196_T2', key: 'filters.plug.type2' },
  { v: 'IEC_62196_T2_COMBO', key: 'filters.plug.ccs' },
  { v: 'CHADEMO', key: 'filters.plug.chademo' },
] as const

// Simple date formatter for the price/availability timestamps — we render the
// relative age in the cell, the tooltip shows the absolute local time.
function fmtAbsolute(iso: string | null): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return ''
  }
}

// loadQuery assembles the API params from the table's UI state. Keeps the
// reactive component lean: every effect just calls this.
function loadQuery(
  q: string,
  filters: Filters,
  sort: SortKey,
  desc: boolean,
  page: number,
  pageSize: PageSize,
): ChargerListParams {
  return {
    q: q.trim() || undefined,
    source: filters.source || undefined,
    plug: filters.plug || undefined,
    current: filters.current || undefined,
    min_power: filters.minPower || undefined,
    max_power: filters.maxPower || undefined,
    available: filters.available || undefined,
    has_price: filters.hasPrice || undefined,
    include_private: filters.includePrivate || undefined,
    sort,
    desc,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  }
}

interface Filters {
  source: string
  plug: string
  current: '' | 'AC' | 'DC'
  minPower: number
  maxPower: number
  available: boolean
  hasPrice: boolean
  includePrivate: boolean
}

const EMPTY_FILTERS: Filters = {
  source: '',
  plug: '',
  current: '',
  minPower: 0,
  maxPower: 0,
  available: false,
  hasPrice: false,
  includePrivate: false,
}

export function ChargersPage() {
  const { t } = useTranslation()

  // Inputs: search (debounced), filter chips, sort, paging.
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS)
  const [sort, setSort] = useState<SortKey>('id')
  const [desc, setDesc] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<PageSize>(50)

  // The page's source-health sidebar doubles as the source filter: it
  // shows one pill per known CPO, with the live charger/price/availability
  // counts. Selecting one filters the table; clicking "All" clears it.
  const [sources, setSources] = useState<SourceHealth[]>([])

  const [rows, setRows] = useState<ChargerListRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)

  // Debounce the search input so the table doesn't re-fetch on every keystroke.
  useEffect(() => {
    const h = setTimeout(() => setSearch(searchInput), 250)
    return () => clearTimeout(h)
  }, [searchInput])

  // Sources are loaded once and refreshed on the same cadence as the source
  // page, so the filter pills always reflect the live state.
  useEffect(() => {
    let alive = true
    const load = () =>
      api
        .status()
        .then((d: StatusResponse) => alive && setSources(d.sources))
        .catch(() => {})
    load()
    const h = setInterval(load, 30000)
    return () => {
      alive = false
      clearInterval(h)
    }
  }, [])

  // Re-fetch the current page when any input changes. `page` resets to 1
  // implicitly on filter/sort/search change via the effect's reset below.
  useEffect(() => {
    let alive = true
    setLoading(true)
    setErr(false)
    api
      .chargerList(loadQuery(search, filters, sort, desc, page, pageSize))
      .then((d) => {
        if (!alive) return
        setRows(d.results)
        setTotal(d.total)
        setLoading(false)
      })
      .catch(() => {
        if (!alive) return
        setErr(true)
        setLoading(false)
      })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search, filters, sort, desc, page, pageSize])

  // When the user changes any filter/search/sort, jump back to page 1 — the
  // result set shifts and the old page number is meaningless.
  useEffect(() => {
    setPage(1)
  }, [search, filters, sort, desc, pageSize])

  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const showingFrom = total === 0 ? 0 : (page - 1) * pageSize + 1
  const showingTo = Math.min(total, page * pageSize)

  // Header cell: click cycles the column → reversed → back to server order.
  const TH = ({ k, label, num }: { k: SortKey; label: string; num?: boolean }) => {
    const active = sort === k
    const arrow = active ? (desc ? ' ▼' : ' ▲') : ''
    const onClick = () => {
      if (!active) {
        setSort(k)
        setDesc(DESC_FIRST.has(k))
      } else if (desc === DESC_FIRST.has(k)) {
        setDesc(!desc)
      } else {
        setSort('id')
        setDesc(false)
      }
    }
    return (
      <th className={`sortable${num ? ' num' : ''}${active ? ' sorted' : ''}`} onClick={onClick} role="button" tabIndex={0}
          onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && onClick()}>
        {label}{arrow}
      </th>
    )
  }

  const activeSource = sources.find((s) => s.id === filters.source)

  return (
    <div className="chargers-page">
      <div className="chargers-inner">
        <div className="chargers-head">
          <h1>{t('chargers.title')}</h1>
          <p className="chargers-sub">
            {t('chargers.summary', { count: total })}
            {loading && <> · {t('chargers.loading')}</>}
          </p>
        </div>

        <div className="chargers-toolbar">
          <input
            className="chargers-search"
            type="search"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder={t('chargers.searchPlaceholder')}
            aria-label={t('chargers.searchPlaceholder')}
          />
          <button
            className={`chip ${hasActiveFilter(filters) ? 'on' : ''}`}
            aria-expanded={filtersOpen}
            onClick={() => setFiltersOpen((o) => !o)}
          >
            {t('chargers.filters')}{hasActiveFilter(filters) ? ` · ${activeFilterCount(filters)}` : ''} {filtersOpen ? '▾' : '▸'}
          </button>
          {(search !== '' || hasActiveFilter(filters)) && (
            <button
              className="chip"
              onClick={() => {
                setSearchInput('')
                setSearch('')
                setFilters(EMPTY_FILTERS)
              }}
            >
              {t('chargers.clear')}
            </button>
          )}
          <span className="chargers-pagesize">
            <label>
              {t('chargers.pageSize')}
              <select value={pageSize} onChange={(e) => setPageSize(Number(e.target.value) as PageSize)}>
                <option value={25}>25</option>
                <option value={50}>50</option>
                <option value={100}>100</option>
              </select>
            </label>
          </span>
        </div>

        {filtersOpen && (
          <div className="chargers-filters">
            <label className={`chip ${filters.source ? 'on' : ''}`}>
              <span>{t('chargers.colSource')}</span>
              <select
                value={filters.source}
                onChange={(e) => setFilters({ ...filters, source: e.target.value })}
              >
                <option value="">{t('chargers.anySource')}</option>
                {sources.map((s) => (
                  <option key={s.id} value={s.id}>{s.name || s.id} ({s.chargers.toLocaleString()})</option>
                ))}
              </select>
            </label>
            <label className={`chip ${filters.plug ? 'on' : ''}`}>
              <select value={filters.plug} onChange={(e) => setFilters({ ...filters, plug: e.target.value })}>
                {PLUGS.map((p) => (
                  <option key={p.v} value={p.v}>{t(p.key)}</option>
                ))}
              </select>
            </label>
            <span className="chip">
              <select
                value={filters.current}
                onChange={(e) => setFilters({ ...filters, current: e.target.value as '' | 'AC' | 'DC' })}
                aria-label={t('chargers.colCurrent')}
              >
                <option value="">{t('chargers.anyCurrent')}</option>
                <option value="AC">AC</option>
                <option value="DC">DC</option>
              </select>
            </span>
            <label className={`chip ${filters.minPower ? 'on' : ''}`}>
              <span>{t('chargers.minPower')}</span>
              <input
                type="number" min={0} step={1} inputMode="numeric"
                value={filters.minPower || ''}
                onChange={(e) => setFilters({ ...filters, minPower: Math.max(0, Number(e.target.value) || 0) })}
                style={{ width: 64 }}
              />
              <span>kW</span>
            </label>
            <label className={`chip ${filters.maxPower ? 'on' : ''}`}>
              <span>{t('chargers.maxPower')}</span>
              <input
                type="number" min={0} step={1} inputMode="numeric"
                value={filters.maxPower || ''}
                onChange={(e) => setFilters({ ...filters, maxPower: Math.max(0, Number(e.target.value) || 0) })}
                style={{ width: 64 }}
              />
              <span>kW</span>
            </label>
            <button
              className={`chip ${filters.available ? 'on' : ''}`}
              onClick={() => setFilters({ ...filters, available: !filters.available })}
            >
              {filters.available ? '✓ ' : ''}{t('chargers.availableOnly')}
            </button>
            <button
              className={`chip ${filters.hasPrice ? 'on' : ''}`}
              onClick={() => setFilters({ ...filters, hasPrice: !filters.hasPrice })}
            >
              {filters.hasPrice ? '✓ ' : ''}{t('chargers.hasPrice')}
            </button>
            <button
              className={`chip ${filters.includePrivate ? 'on' : ''}`}
              onClick={() => setFilters({ ...filters, includePrivate: !filters.includePrivate })}
            >
              {filters.includePrivate ? '✓ ' : ''}{t('chargers.includePrivate')}
            </button>
            {activeSource && (
              <span className="muted chargers-filters-hint">
                {t('chargers.sourceHint', { name: activeSource.name || activeSource.id })}
              </span>
            )}
          </div>
        )}

        {err ? (
          <div className="state">{t('chargers.error')}</div>
        ) : (
          <div className="chargers-table-wrap">
            <table className="chargers-table">
              <thead>
                <tr>
                  <TH k="id" label={t('chargers.colId')} num />
                  <TH k="name" label={t('chargers.colName')} />
                  <TH k="city" label={t('chargers.colCity')} />
                  <TH k="source" label={t('chargers.colSource')} />
                  <TH k="current" label={t('chargers.colCurrent')} />
                  <TH k="plug" label={t('chargers.colPlug')} />
                  <TH k="power" label={t('chargers.colPower')} num />
                  <TH k="price" label={t('chargers.colPrice')} num />
                  <TH k="available" label={t('chargers.colAvailable')} num />
                  <TH k="updated" label={t('chargers.colUpdated')} />
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const conf = sourceConfidence(r.source_type)
                  return (
                    <tr key={r.id}>
                      <td className="num"><a href={`/charger/${r.id}`} className="chargers-id">{r.id}</a></td>
                      <td>
                        <a href={`/charger/${r.id}`} className="chargers-name">{r.name || '—'}</a>
                        {r.address && <div className="muted chargers-sub">{r.address}</div>}
                        {r.private && <div className="muted chargers-sub">{t('chargers.private')}</div>}
                      </td>
                      <td>{r.city || '—'}{r.postal_code ? <span className="muted"> · {r.postal_code}</span> : null}</td>
                      <td>
                        {r.source || r.cpo_id}
                        <div className="muted chargers-sub">
                          <span className={`conf conf-${conf}`}>{t(`confidence.${conf}`)}</span>
                          {r.country && <> · {r.country}</>}
                        </div>
                      </td>
                      <td>{r.current_type || '—'}</td>
                      <td>{plugLabel(r.plug_type)}</td>
                      <td className="num">{r.power_kw ? r.power_kw.toFixed(r.power_kw >= 100 ? 0 : 1) : '—'}</td>
                      <td className="num" title={fmtAbsolute(r.price_updated_at)}>{eur(r.comparable_price_eur)}</td>
                      <td className="num" title={fmtAbsolute(r.status_updated_at)}>
                        {r.available_count == null ? <span className="muted">—</span> : r.available_count > 0 ? <span className="chargers-free">{r.available_count}</span> : 0}
                      </td>
                      <td className="muted" title={fmtAbsolute(r.status_updated_at)}>{r.status_updated_at ? new Date(r.status_updated_at).toLocaleDateString() : '—'}</td>
                    </tr>
                  )
                })}
                {!loading && rows.length === 0 && (
                  <tr><td colSpan={10} className="muted chargers-empty">{t('chargers.noMatches')}</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}

        <div className="chargers-pager">
          <span className="muted">
            {total === 0 ? t('chargers.noResults') : t('chargers.showingRange', { from: showingFrom, to: showingTo, total: total.toLocaleString() })}
          </span>
          <div className="chargers-pager-buttons">
            <button className="btn" disabled={page <= 1} onClick={() => setPage(1)}>«</button>
            <button className="btn" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>‹</button>
            <span className="chargers-pager-page">{t('chargers.pageOf', { page, total: pageCount })}</span>
            <button className="btn" disabled={page >= pageCount} onClick={() => setPage((p) => Math.min(pageCount, p + 1))}>›</button>
            <button className="btn" disabled={page >= pageCount} onClick={() => setPage(pageCount)}>»</button>
          </div>
        </div>
      </div>
    </div>
  )
}

function hasActiveFilter(f: Filters): boolean {
  return f.source !== '' || f.plug !== '' || f.current !== '' || f.minPower > 0 || f.maxPower > 0 ||
    f.available || f.hasPrice || f.includePrivate
}

function activeFilterCount(f: Filters): number {
  return (f.source ? 1 : 0) + (f.plug ? 1 : 0) + (f.current ? 1 : 0) + (f.minPower ? 1 : 0) +
    (f.maxPower ? 1 : 0) + (f.available ? 1 : 0) + (f.hasPrice ? 1 : 0) + (f.includePrivate ? 1 : 0)
}
