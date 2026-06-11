# charging — cheapest public EV charger nearby

Live at **[charging.appmire.be](https://charging.appmire.be)**.

A service that ingests open EV-charging data from Belgium's National Access
Point and answers:

1. **Where is the best-value *available* public charger near me?** — ranked by
   the real cost of *your* charge (energy + time + session fee) plus the
   **detour** to get there.
2. **How has a charger's ad-hoc price changed over time?** (statistics)
3. **What do other drivers report** about a charger (out of service, not really
   public, slower than advertised, …)?

## Why this exists / where the data comes from

Under **AFIR Article 20** (Regulation EU 2023/1804), since **2025-04-14** every
Belgian CPO with paid public/semi-public chargers must publish static **and**
dynamic data — location, connectors, power, real-time availability, and the
**ad-hoc price** — free of charge via open APIs, catalogued on the National
Access Point **[transportdata.be](https://www.transportdata.be/)**. Most feeds
speak **OCPI** (DATEX II becomes mandatory 2026-04-14).

The NAP is a *catalogue of per-CPO endpoints*, not one unified API, so we
aggregate the feeds. See [Sources](#sources--getting-real-data).

> Ad-hoc price ≠ per-charge-card price. AFIR mandates the drive-up price, which
> is the fairest basis for comparison. Per-MSP/card tariffs would require a
> commercial source.

## Architecture (API/CLI-driven)

The HTTP API is the single control + data plane; every client goes through it —
nothing else touches the database. **Pricing + ranking run on the client**: the
API does a fast indexed geo query and returns candidates with their structured
tariff; the PWA prices them for the user's car at the current time and ranks by
price + detour (see [Comparison model](#comparison-model)).

```
   PWA (web/, charging.appmire.be)  ─┐
   chargingctl (CLI)                 ├─▶  HTTP API (cmd/api)  ──┐
   third parties / OCPI CPOs ────────┘     read · admin · OCPI │
                                                               ▼
   cmd/ingest (scheduler) ──poll/crawl──▶  PostgreSQL+PostGIS ◀── cmd/migrate
   OCPI files · OCPI API · DATEX · Monta     SCD2 history · geo · stats · reports
```

In production (single VM) nginx terminates TLS and reverse-proxies the PWA and
the API (`/api/*`) on one origin; the API serves the bulk **export** and the
**OCPI eMSP** endpoints too. See [Deploy](#deploy).

## Components

| Path | What |
|---|---|
| `cmd/api` | HTTP API: read endpoints, hidden admin control plane, OCPI eMSP, `/export`, Scalar `/docs` |
| `cmd/ingest` | polling scheduler (availability + price) + Monta background crawl |
| `cmd/migrate` | applies embedded migrations on deploy |
| `cmd/chargingctl` | CLI client over the API (read + admin) |
| `internal/ocpi` | OCPI 2.1.1/2.2.1 **client** (pull) + **eMSP server** (handshake + push receiver) |
| `internal/{datex,monta}` | DATEX II reader; Monta Public API (AFIR list + per-EVSE price/status) |
| `internal/{normalize,model,pricing}` | canonical model, plug/private classification, comparable-session pricing |
| `internal/{report,export}` | community reports taxonomy/aggregation; open bulk dataset dumps |
| `internal/{store,ingest,source}` | persistence, CDC engine, source registry |
| `web/` | React + Vite + TS **PWA**; client-side pricing in `web/src/pricing.ts` |
| [`docs/sources.md`](docs/sources.md) | Belgian NAP source catalogue + status |
| [`docs/ocpi.md`](docs/ocpi.md) | acting as an OCPI eMSP: handshake + push |
| [`docs/export.md`](docs/export.md) | the open bulk dataset dumps under `/export` |
| [`docs/frontend.md`](docs/frontend.md) | PWA UX + features |
| [`docs/access-request-emails.md`](docs/access-request-emails.md) | API-key request drafts |

## Comparison model

The cost of charging anywhere reduces to **energy needed + speed + the
per-session fee**, so the user just sets a **charging profile** (how much energy,
how fast) and their **car** (usable battery, consumption) — all stored in the
browser (localStorage), editable via the ⚙ settings panel. The 10 fixed
comparison profiles still exist *server-side* for the Insights aggregates; the
user-facing picker is energy + speed.

The result list is **the 50 lowest by weighted cost = charging price + detour**,
not strictly the nearest or the raw-cheapest:

- **Per-car, time-of-day price**: energy×€/kWh (with charging losses) + duration
  (energy ÷ speed, DC taper) ×€/h + the FLAT session fee, evaluated at the
  current time so peak/off-peak tariffs rank correctly.
- **Detour**: the round-trip to reach a charger adds extra energy (consumption ×
  km × a reference €/kWh) and time (× a value-of-time €/h) — a cheaper charger
  far off your route isn't actually cheaper. Toggle + tune in settings.
- **Private chargers excluded**: home / peer-to-peer points (e.g. Stroohm
  "Private"/"Home") are filtered from the public search by default (a filter
  re-includes them).
- **Community reports** corroborated by ≥2 drivers (out-of-service / not-public /
  doesn't-exist) sink a charger to the bottom (never hidden).

Pricing is computed **on the client** (`web/src/pricing.ts`), a faithful port of
`internal/pricing` — so changing the car/profile/detour re-ranks instantly with
no network call. **`internal/pricing` is canonical; keep `pricing.ts` in sync.**

## Key design decisions

- **Prices change rarely → temporal versioning (SCD Type 2), not snapshots.**
  A new `tariff_version` row is written **only when the tariff content changes**
  (order-independent content hash). "Current price" is one indexed row; "price
  at time T" is a temporal range query.
- **Manufactured history.** OCPI feeds expose only the *current* tariff; we build
  history by polling and recording observed changes — see the honesty rules.
- **Availability is current-only** (overwritten each poll), by design.
- **PostgreSQL + PostGIS** for geo (`ST_DWithin` + KNN), relational data and
  temporal history in one store.
- **Canonical values**: plug standards are normalized to OCPI form on ingest
  (`IEC_62196_T2` …, shown as "Type 2", "CCS", …); private chargers flagged from
  the operator name.
- **Store layer uses pgx directly** (not sqlc): PostGIS `geography`/`numeric`
  fight code generation.
- **OpenAPI is generated from the typed handlers (huma)** so it can't drift;
  served as Scalar at `/docs`, spec at `/openapi.{json,yaml}`. The admin control
  plane is registered but **hidden** from the public spec.

### Running continuously (unattended)

- **Two cadences:** availability frequently (`status_cron`, ~3 min); price/tariff
  diffs rarely (`poll_cron`, ~daily). **Monta** has no bulk price endpoint, so a
  background crawl cycles its per-EVSE status+price under the rate limit (private
  chargers skipped to save budget).
- **Staleness:** availability older than `AVAILABILITY_STALE_AFTER` (15 min) is
  *unknown* — excluded from `available=true`.
- **Overlap-safe**, **hot registry reload** (every 5 min), **graceful shutdown**.
- **Observability:** Prometheus `/metrics`; `/readyz` fails if an enabled source
  has no recent successful ingest.

### Data honesty rules

- `observed_from/to` are *when we saw the change*, not the CPO's real change
  time (`source_last_updated` keeps OCPI `last_updated` when given).
- A CPO endpoint being down is a **gap**, never "price removed"/"free".
- "No tariff published" ≠ "price = €0".
- Detour assumptions (reference €/kWh, value-of-time) are user-set and surfaced,
  never hidden.

## Quick start

Requires Docker, Go 1.26+, pnpm (for the web).

```bash
cp .env.example .env
make db-up && make db-wait     # start PostGIS
make migrate                   # apply schema
make demo-seed                 # OPTIONAL: fake data so the API returns results
make run-api                   # serve on :8080
```

```bash
curl 'localhost:8080/healthz'
curl 'localhost:8080/chargers/nearby?lat=51.0544&lon=3.7251'        # geo candidates (+ tariffs) for client pricing
curl 'localhost:8080/chargers/cheapest?lat=51.0544&lon=3.7251&energy_kwh=40'  # server-ranked
open http://localhost:8080/docs                                      # interactive API reference
```

### Frontend (PWA)

Map-first installable PWA in [`web/`](web/) (React + Vite + TS + Leaflet). Pure
API client; URLs are shareable (`/@lat,lon,zoom`, `/charger/{id}/{slug}`).

```bash
cd web && cp .env.example .env && pnpm install && pnpm dev
```

See [`docs/frontend.md`](docs/frontend.md).

## API

Full, always-current reference: **`/docs`** (Scalar) / **`/openapi.yaml`**.
Public endpoints:

| Method | Path | Description |
|---|---|---|
| GET | `/chargers/nearby` | nearest candidates + structured tariffs (the PWA prices/ranks these) |
| GET | `/chargers/cheapest` | server-ranked cheapest-by-price+detour |
| GET | `/chargers/{id}` | one charger (shareable deep links) |
| GET | `/chargers/{id}/price-history` | every recorded tariff version |
| GET | `/chargers/{id}/live` | on-demand live availability (Monta) |
| GET/POST | `/chargers/{id}/reports` | community reports (read / submit) |
| GET | `/reports/types` | the structured report taxonomy |
| GET | `/sessions` | the comparison-session profiles |
| GET | `/stats/{overview,sessions,regions,price-trend}` | market statistics |
| GET | `/export/…` | open bulk dataset dumps ([docs](docs/export.md)) |
| GET | `/healthz` · `/readyz` · `/metrics` | ops |
| — | `/ocpi/…` | OCPI 2.2.1 eMSP (handshake + push receiver, [docs](docs/ocpi.md)) |

`/chargers/{nearby,cheapest}` filters: `lat`,`lon` (required), `radius`,
`min_power`, `plug`, `available`, `include_private`, `limit`. `cheapest` also
takes the car + session + detour params (`energy_kwh`,`power_kw`,`usable_kwh`,
`consumption_kwh100`,`detour`,`detour_price`,`detour_eur_per_h`, or a named
`session`); the PWA passes the car/session to `nearby` only as the tariffs it
needs and computes the rest locally.

The **admin control plane** (`/admin/*`, bearer `ADMIN_TOKEN`) is functional but
**hidden from the public OpenAPI**: list/add/enable/disable/token/delete sources,
trigger ingests, view runs, clear a charger's reports, and run the OCPI
credentials handshake (`POST /admin/sources/{id}/ocpi/register`).

### `chargingctl`

Everything is operable through the API; `chargingctl` is a thin client over it.

```bash
export CHARGING_API=http://localhost:8080 ADMIN_TOKEN=…
chargingctl chargers cheapest --lat 51.05 --lon 3.72 --session charge1080_dc300
chargingctl stats price-trend --months 12
chargingctl sources set-token energyvision "$TOKEN" && chargingctl sources enable energyvision
```

## Sources & getting real data

| Source | Type | Status | Notes |
|---|---|---|---|
| **Road** | open OCPI 2.2.1 files | **live, no key** | ~7,700 connectors incl. ad-hoc prices; enabled by default |
| **Monta** | AFIR list + per-EVSE API | **live** (key) | open locations; per-EVSE price/status crawl; `MONTA_CREDS` |
| **EnergyVision** | OCPI 2.1.1/eMSP | pending | request a token, or run the OCPI handshake ([docs/ocpi.md](docs/ocpi.md)) |
| **Eco-Movement** | commercial Data API | pending | has ad-hoc CPO prices (~36k connectors); token-gated, custom schema |
| any CPO | OCPI eMSP | ready | we can register + receive pushes ([docs/ocpi.md](docs/ocpi.md)) |

Road + Monta already populate the map. To add a per-CPO OCPI source with a token:

```bash
chargingctl sources set-token <id> "$TOKEN" && chargingctl sources enable <id>
```

Or, for a CPO that speaks OCPI, run the credentials handshake — see
[`docs/ocpi.md`](docs/ocpi.md). New sources: extend `source.Seeds()` or
`chargingctl sources add`.

## Deploy

`docker-compose.prod.yml` runs the whole stack (PostGIS + migrate + api + ingest
+ web). Live deployment notes (arm64 host, nginx `/api` reverse proxy, env) are
in the [appmire4-web deploy memory]; in short:

```bash
cp .env.example .env   # set ADMIN_TOKEN, MONTA_CREDS, PUBLIC_URL, WEB_API_BASE…
make prod-up           # build + start; migrations run automatically
```

`PUBLIC_URL` (e.g. `https://charging.appmire.be/api`) makes the OCPI endpoints'
advertised URLs absolute. `WEB_API_BASE` is the API origin the **browser**
reaches; it's injected into the web container at runtime (envsubst → `/config.js`).

## Testing

```bash
make test
```

`internal/{pricing,report,model,ocpi,datex,monta}` have unit tests (pricing
time-of-day + custom sessions; report TTL/opposite/threshold; plug + private
classification; OCPI handshake + push receiver). `internal/ingest` runs the full
pipeline against a mock OCPI server + PostGIS (SCD2 behaviour, cheapest-nearby);
DB-backed tests skip cleanly without a database.

## Status

Live in production with Road + Monta. Built: client-side pricing + detour
ranking, car/charging-profile settings, community reports, private-charger
filtering, plug normalization, shareable URLs, the open `/export` dumps, and an
OCPI 2.2.1 eMSP (handshake + push) ready for direct CPO integrations.

Next: connect EnergyVision (OCPI handshake or their forthcoming consumer API)
and Eco-Movement (priced API); DATEX II coverage expansion (mandatory
2026-04-14); CI guard that `pricing.ts` matches `pricing.go`.
