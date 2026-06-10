import { useEffect, useState } from 'react'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, type Charger, type PricePoint } from './api'
import { AvailBadge, availOf, ago, eur } from './ui'

export function ChargerDetail({ charger, onClose }: { charger: Charger; onClose: () => void }) {
  const [history, setHistory] = useState<PricePoint[] | null>(null)

  useEffect(() => {
    let alive = true
    api.priceHistory(charger.id).then((r) => alive && setHistory(r.history)).catch(() => alive && setHistory([]))
    return () => {
      alive = false
    }
  }, [charger.id])

  const chart = (history ?? [])
    .filter((h) => h.comparable_price_eur != null)
    .slice()
    .reverse()
    .map((h) => ({ t: h.observed_from.slice(0, 10), price: h.comparable_price_eur as number }))

  const matrix = Object.entries(charger.comparable_prices).sort(([a], [b]) => a.localeCompare(b))
  const nav = `https://www.openstreetmap.org/directions?to=${charger.lat},${charger.lon}`

  return (
    <div className="overlay" onClick={onClose}>
      <div className="detail" onClick={(e) => e.stopPropagation()}>
        <div className="detail-head">
          <div>
            <h2>{charger.name || 'Charger'}</h2>
            <div className="muted">{charger.address || `${charger.cpo_id}`}</div>
          </div>
          <button className="btn" onClick={onClose}>Close</button>
        </div>

        <div style={{ marginTop: 8 }}>
          <AvailBadge a={availOf(charger)} /> <span className="muted">· updated {ago(charger.status_updated_at)}</span>
        </div>

        <div className="kv">
          <div className="cell"><div className="k">Price (this session)</div><div className="v">{eur(charger.session_price_eur ?? charger.comparable_price_eur)}</div></div>
          <div className="cell"><div className="k">Power</div><div className="v">{charger.power_kw} kW {charger.current_type}</div></div>
          <div className="cell"><div className="k">Plug</div><div className="v">{charger.plug_type || '—'}</div></div>
          <div className="cell"><div className="k">Distance</div><div className="v">{Math.round(charger.distance_m)} m</div></div>
        </div>

        <h3 style={{ margin: '6px 0' }}>Price history</h3>
        {history == null ? (
          <div className="state"><div className="spinner" />loading…</div>
        ) : chart.length < 2 ? (
          <div className="muted" style={{ fontSize: 13 }}>Not enough history yet — we record a point whenever the tariff changes.</div>
        ) : (
          <ResponsiveContainer width="100%" height={180}>
            <LineChart data={chart} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
              <XAxis dataKey="t" fontSize={11} />
              <YAxis fontSize={11} width={42} domain={['auto', 'auto']} />
              <Tooltip formatter={(v: number) => eur(v)} />
              <Line type="stepAfter" dataKey="price" stroke="#15803d" strokeWidth={2} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        )}

        {matrix.length > 0 && (
          <>
            <h3 style={{ margin: '12px 0 6px' }}>All sessions</h3>
            <table className="matrix">
              <tbody>
                {matrix.map(([k, v]) => (
                  <tr key={k}>
                    <td>{k}</td>
                    <td className="num">{eur(v)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}

        <p className="caveat">Drive-up (ad-hoc) price — your charge card may differ.</p>
        <a className="btn primary" href={nav} target="_blank" rel="noreferrer" style={{ display: 'inline-block', marginTop: 6 }}>
          Navigate
        </a>
      </div>
    </div>
  )
}
