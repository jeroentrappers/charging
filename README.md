# charging — cheapest public EV charger nearby

A service that ingests open EV-charging data from Belgium's National Access
Point and answers two questions:

1. **Where is the cheapest *available* public charger near me?**
2. **How has a charger's ad-hoc price changed over time?** (statistics)

## Why this exists / where the data comes from

Under **AFIR Article 20** (Regulation EU 2023/1804), since **2025-04-14** every
Belgian CPO with paid public/semi-public chargers must publish static **and**
dynamic data — location, connectors, power, real-time availability, and the
**ad-hoc price** — free of charge via open APIs, catalogued on the National
Access Point **[transportdata.be](https://www.transportdata.be/)**. Most feeds
speak **OCPI** (DATEX II becomes mandatory 2026-04-14).

The NAP is a *catalogue of per-CPO endpoints*, not one unified API, so we
aggregate the feeds ourselves. The first wired source is **EnergyVision**
(OCPI 2.1.1). See **[Getting real data](#getting-real-data)**.

> Ad-hoc price ≠ per-charge-card price. AFIR mandates the drive-up price, which
> is the fairest basis for comparison. Per-MSP/card tariffs (e.g. a Mobiflow
> card) would require a commercial source like the Chargeprice API.

## Key design decisions

- **Prices change rarely → temporal versioning (SCD Type 2), not snapshots.**
  A new `tariff_version` row is written **only when the tariff content changes**
  (detected via an order-independent content hash). The history table stays
  tiny; "current price" is a single indexed row; "price at time T" is a
  temporal range query.
- **Manufactured history.** The OCPI feeds only ever expose the *current*
  tariff. We build history by polling and recording observed changes — see
  the honesty rules below.
- **Availability is current-only** (overwritten each poll), by design. Price is
  historized; occupancy is not.
- **PostgreSQL + PostGIS** for geo ("nearby" via `ST_DWithin` + KNN), relational
  data, and temporal history in one store.
- **Comparable price.** Raw OCPI tariffs mix €/kWh, €/min and session fees, so
  they aren't directly sortable. We compute the cost of a fixed **standard
  session (add 30 kWh at the connector's power)** into `comparable_price_eur`.
  This makes AC and DC chargers comparable and "cheapest" well-defined.
- **Store layer uses pgx directly** (not sqlc): PostGIS `geography` + `numeric`
  fight code generation, and explicit geo expressions/casts are cleaner.

### Data honesty rules (so statistics stay credible)

- `observed_from`/`observed_to` are *when we saw the change*, not the CPO's real
  change time. OCPI `last_updated` is kept as `source_last_updated` when given.
- A CPO endpoint being down is a **gap**, never "price removed" or "free".
- "No tariff published" is distinct from "price = €0" (the former records no
  version at all).

## Architecture

```
cmd/ingest   poller binary: -once (cron/CI) or in-process scheduler
cmd/api      HTTP API: cheapest-nearby + price-history

internal/ocpi       OCPI 2.1.1 client (paginated Locations + Tariffs, Token auth)
internal/normalize  OCPI -> canonical model
internal/model      canonical types + tariff content hash
internal/pricing    comparable standard-session price from tariff components
internal/source     CPO registry + token resolution (+ EnergyVision seed)
internal/store      pgx + PostGIS persistence and queries
internal/ingest     change-data-capture engine (hash -> SCD2), run logging
db/migrations       goose schema
```

### Data model (core tables)

- `cpo` — source registry (OCPI base URL, token env var, poll cron, enabled).
- `charger` — one row per connector; `geography(Point,4326)` + GiST index.
- `tariff_version` — append-only history; partial unique index
  `WHERE observed_to IS NULL` guarantees exactly one current row per charger.
- `charger_status` — current availability only.
- `ingest_run` — one row per poll per CPO (rows seen, changes, error).

## Quick start

Requires Docker, Go 1.26+.

```bash
cp .env.example .env
make db-up && make db-wait     # start PostGIS
make migrate                   # apply schema
make demo-seed                 # OPTIONAL: fake data so the API returns results
make run-api                   # serve on :8080
```

Then:

```bash
curl 'localhost:8080/healthz'
curl 'localhost:8080/chargers/cheapest?lat=51.0544&lon=3.7251&radius=5000&available=true'
curl 'localhost:8080/chargers/1/price-history'
```

### API

| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | liveness + DB ping |
| GET | `/chargers/cheapest` | nearby chargers, cheapest first |
| GET | `/chargers/{id}/price-history` | every recorded tariff version |

`/chargers/cheapest` query params: `lat`, `lon` (required), `radius` (m,
default 5000), `min_power` (kW), `plug` (OCPI standard), `available`
(`true`/`1`), `limit` (default 50). Results are ordered by
`comparable_price_eur` (nulls last), then distance.

## Getting real data

The EnergyVision feed needs a free OCPI key.

> **ACTION REQUIRED:** email **myevplatform@energyvision.be** to request an OCPI
> API key (granted free / non-discriminatory under AFIR Article 20).

Once you have it:

```bash
echo 'ENERGYVISION_TOKEN=<your-key>' >> .env
# enable the source:
docker compose exec db psql -U charging -d charging \
  -c "UPDATE cpo SET enabled=true WHERE id='energyvision';"

make run-ingest-once   # one pass
# or: make run-ingest  (scheduler; daily per the source's poll_cron)
```

Add more Belgian CPOs by extending `source.Seeds()` (or inserting `cpo` rows)
with their NAP-published OCPI base URLs and token env vars.

## Testing

```bash
make test
```

`internal/pricing` has pure unit tests (incl. off-peak/time-of-day, kWh
thresholds, order-independent hashing). `internal/ingest` has an end-to-end
integration test that runs the whole pipeline against a mock OCPI server and the
PostGIS database, asserting SCD2 behaviour (no new version when unchanged; a new
version + closure on change) and the cheapest-nearby geo query. It **skips
cleanly** if no database is reachable.

## Status & roadmap

Working vertical slice: OCPI client → normalize → comparable pricing → SCD2
storage → cheapest-nearby + history API, proven end-to-end against PostGIS.

Not yet done (natural next steps):
- Live EnergyVision ingestion (blocked only on the API key above).
- More CPO sources; DATEX II reader (mandatory 2026-04-14).
- Recompute the comparable price at request time for time-varying tariffs (the
  stored value uses a fixed reference time for trend comparability).
- Aggregate statistics endpoints (avg €/kWh by region over time).
- Frontend / map UI.
