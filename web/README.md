# charging-web

Installable **PWA** frontend (React + Vite + TypeScript + Leaflet) for the
charging API. Map-first, mobile-first, fully responsive (bottom-sheet list on
phones, side-by-side map+list on desktop). See [`../docs/frontend.md`](../docs/frontend.md)
for the UX analysis.

## Run

```bash
cp .env.example .env          # set VITE_API_BASE to your API origin
pnpm install
pnpm dev                      # dev server (default http://localhost:5173)
pnpm build && pnpm preview    # production build + preview
```

The API must be running with CORS allowing this origin (it defaults to `*`):

```bash
# from the repo root
make db-up && make migrate && make demo-seed   # demo data
make run-api                                    # API on :8080
```

## What's here

- **Find** — map of nearby chargers coloured by price + a ranked list; a
  **session selector** ("Best price / 100 km top-up / 10→80%" × AC/DC speeds)
  that drives the comparison; filters (available, power, plug); geolocation.
- **Charger detail** — info, three-state availability with freshness, the full
  session price matrix, and a **price-history chart**.
- **Insights** — market overview (AC vs DC), price trend, cheapest regions,
  per-session averages.

## Notes

- **PWA**: installable, offline app shell; API + OSM tiles are runtime-cached
  (`vite-plugin-pwa`).
- **Fast**: the charting library is code-split, so the initial Find view ships
  ~95 KB gzip; map tiles and charts load on demand.
- **Honest by design**: stale availability is shown as *Unknown* (never "free"),
  and a persistent caveat notes these are drive-up (ad-hoc) prices.
- **Runtime-configurable API origin**: the Docker image is built once; on
  startup it renders `/config.js` from the `VITE_API_BASE` env var (via
  `envsubst`), which the app reads (`window.__CONFIG__.apiBase`). No rebuild per
  environment. In `pnpm dev`, `config.js` is empty so it falls back to
  `import.meta.env.VITE_API_BASE` from `.env`.
- Deploy as static files to any host (separate origin from the API).
