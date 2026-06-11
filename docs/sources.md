# NAP charging data sources (BE + NL/DE/FR)

Catalogue of EV-charging feeds published on the National Access Points under
AFIR Article 20, with what each takes to consume and how to request access.
Adding a source to the running system = insert a `cpo` row (+ token env) — the
scheduler hot-reloads it.

Last researched: 2026-06-09 (BE), 2026-06-11 (NL/DE/FR expansion).

## Beyond Belgium — wired 2026-06-11 (no paid feeds)

The same AFIR rule means each country's NAP is the backbone. Map coverage is
broadly free; **ad-hoc price is the scarce thing** (only NL is free + structured
today). Unpriced chargers map fine and show no comparable price (info-only).
See the [[eu-data-sources]] memory for the full landscape.

| Source (`cpo` id) | Country | type | Locations | Status | **Ad-hoc price** | Notes |
|---|---|---|---|---|---|---|
| **NDW · DOT-NL** (`dotnl`) | 🇳🇱 NL | `ocpi_file_gz` | ✅ ~88k | ✅ | ✅ **structured** | Open OCPI 2.2.1 .json.gz (locations+tariffs); ~226k connectors, ~50% priced, incl. Fastned. Daily poll + hourly status. |
| **Bundesnetzagentur** (`bnetza`) | 🇩🇪 DE | `bnetza` | ✅ ~134k | ❌ | ❌ | Official registry CSV (Latin-1/`;`), scraped dated URL. Location-only. Monthly. |
| **transport.data.gouv IRVE** (`irve`) | 🇫🇷 FR | `irve` | ✅ ~230k | ❌ | ❌ (free-text only) | Consolidated GeoJSON, ~585 MB, streamed. Location-only. Monthly. |
| Eco-Movement / Chargeprice | EU | — | ✅ | ✅ | ✅ | **Commercial — deliberately NOT integrated** (no paid feeds). The all-EU priced backbone if that changes. |

DE/FR price will improve as the AFIR **DATEX II** mandate (14 Apr 2026) matures
in Mobilithek (DE) and the IRVE-dynamique feed (FR).

## Which sources expose ad-hoc PRICE? (open-pricing sweep, 2026-06-10)

| Source | Open (no key)? | Ad-hoc price? | Availability? |
|---|---|---|---|
| **Road** | ✅ yes | ✅ **yes** | ✅ yes (status) |
| **Monta** (Public API) | list: ✅ open / price: ⚠️ key | ⚠️ key + **per-EVSE** only | ⚠️ key + per-EVSE |
| EnergyVision | ⚠️ free key (email) | likely (OCPI Tariffs) | ✅ |
| Tesla | ⚠️ key (401 without) | likely (OCPI Tariffs) | ✅ |
| Eco-Movement (NAP feed) | ✅ yes | ❌ no | ❌ no |
| INDIGO | ✅ yes | ❌ no (static) | ❌ no |
| Gireve (EVCI) | ❌ fee-based license | ❌ not in open set | — |

**Conclusion:** **Road is the only fully-open (no-credential) source with ad-hoc
price in bulk** — and it's already live. No other open feed gives bulk price:
Eco-Movement, INDIGO and **Monta's open charge-points list** are all
location-only. **Monta's** price + availability are key-gated and **per-EVSE**
(on-demand), so useful as a live "price for the tapped charger" lookup (with a
key, Monta network only), not for bulk ingestion. Open Charge Map (global, free
key) has only a sparse, unstructured `UsageCost` text field — not comparable
tariffs.

**Monta — NEW Public API** (the Partner API AFIR endpoint is deprecated, sunset
2026-09-08). Probed live 2026-06-10:
- **Charge points (bulk):** `GET https://public-api.monta.com/api/v1/afir/charge-points?country=BE`
  — **OPEN, no auth** (countries BE, DK). **DATEX II serialised as JSON**.
  Location + connectors + power only — **NO price** (verified: no price/tariff/
  currency keys). Paginated (`perPage`). → location coverage, like Eco-Movement.
- **Per-EVSE status:** `GET …/afir/charge-points/{evseId}/status` — returns
  **availability + ad-hoc price**, but **Bearer auth required** and **one call
  per EVSE** (rate limit 100 req / 10 min). Verified: 400 "upstream" without a
  token even for valid BE EVSE ids.
- **So Monta gives no open *bulk* price.** Its price is key-gated + per-EVSE.
- **Adapter built** (`internal/monta`, `source_type='monta'`): OAuth token cache,
  paginated list → connectors, per-EVSE `Status` → availability + ad-hoc tariff
  (dedup tax-incl/excl). **Verified live**: BE = 3,223 connectors (2,548
  Monta-party); status returns e.g. €0.56/kWh, €0.54/kWh + €1 session,
  €0.70/kWh + €48/h. Creds via `MONTA_CREDS="clientId:clientSecret"`.
- **Rate-limit reality:** status is per-EVSE at 100 req/10 min → a full price
  pass over 2,548 EVSEs ≈ **4 hours**. So the bulk feed is **locations-only**;
  **price/availability are fetched on demand** (`monta.Client.Status`) for the
  charger a user opens — the scalable shape. (A slow background price crawl is
  possible but deferred.)

**INDIGO note:** its open static file uses the **same DATEX II profile as
Eco-Movement** (`maxPowerAtSocket`, `facilityLocation>address`, `refillPoint` …),
so our `datex` reader already parses it — but it's **location-only (no price)**,
verified against the actual 1.2 MB file (37 element types, none price-related).

## Feeds

| Provider | Coverage | Format | Endpoint | Consumable now? | Access contact |
|---|---|---|---|---|---|
| **EnergyVision** | 1 CPO | OCPI **2.1.1** | `https://ocpi.energyvision.be/cpo/2.1.1/` | ✅ matches our client | myevplatform@energyvision.be |
| **Tesla Belgium** | 1 CPO (Superchargers) | OCPI **2.2.1** | `https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/` | ⚠️ needs 2.2.1 support | spolireddi@tesla.com |
| **Monta** | 1 CPO | AFIR JSON (OCPI 2.2.1) | `https://docs.partner-api.monta.com/reference/get-afir-charge-points` | ⚠️ needs 2.2.1 / adapter | data@monta.com |
| **Road** ✅LIVE | 1 CPO (~3,300 sites / 7,700 connectors) | OCPI 2.2.1 static JSON | `https://roaming.road.io/files/9ef09c78-2666-418a-aa45-4f2261e2e305/{locations,tariffs}.json` | ✅ **open, no key** — wired & ingesting (incl. prices) | roaming-dev@road.io |
| **Eco-Movement** ⭐ | **~20 networks (~36k connectors)** | **DATEX II** XML (open, token in URL) | `https://api.eco-movement.com/api/nap/datexii/locations?token=…` | ✅ reader works — but **locations + power only, NO price/availability** (≈31 MB) | nap@eco-movement.com |
| **Gireve (EVCI)** | many (roaming) | DATEX II XML | dataset `/en/dataset/evci` | ❌ needs DATEX II reader | via dataset page |
| **Group INDIGO** | 1 CPO | DATEX II XML | dataset `/en/dataset/indigo-open-data-evcharging` | ❌ needs DATEX II reader | via dataset page |

⭐ **Eco-Movement is the highest-leverage source**: one integration covers
Allego, bp pulse, Blink Charging, ChargePoint, Circle K, Dats24, Electra,
Fastned, Gabriels, Interparking, IONITY, Lidl, Litran, Porsche, PowerGo, Shell
Recharge, Sparki, Q8 electric, TotalEnergies. Its NAP feed is **DATEX II** and
the static set may be locations + AFIR specs only — confirm whether **ad-hoc
price + dynamic availability** are included (we need both for history). Their
commercial **OCPI** API is the richer alternative.

## What's needed to consume each format

- **OCPI 2.1.1** — ✅ supported (EnergyVision). Just needs a token.
- **OCPI 2.2.1** — ✅ supported: `/versions` discovery, base64 `Token` auth, and
  2.2.1 fields (`max_electric_power`, `tariff_ids`). Unlocks Tesla + Monta once
  tokens are set.
- **DATEX II** — ✅ reader built (`internal/datex`, v3 EnergyInfrastructure),
  wired via `cpo.source_type='datex'`, and **validated against the live
  Eco-Movement feed** (parses 35,980 connectors). Caveat: that open feed is
  **coverage only — no ad-hoc price, no live status** (≈31 MB), so enable it for
  reach, not for price comparison. Mandatory NAP format from 2026-04-14.
- **Static JSON file** (Road) — ✅ **done** (`source_type='ocpi_file'`): fetches
  `{base}/locations.json` + `{base}/tariffs.json` (bare OCPI arrays). It's **open
  (no token)** and carries real ad-hoc prices, so it's enabled by default and
  ingesting today — the live proof of the whole pipeline before any key arrives.
  (The file's UUID path may rotate; update via `chargingctl sources add road --url …`.)

Seeded (disabled) sources: `energyvision` (OCPI 2.1.1), `tesla` (OCPI 2.2.1),
`ecomovement` (DATEX II). Enable with a token once access is granted.

## Access-request checklist

Each direct CPO needs its own free key (AFIR: non-discriminatory, no cost).
**All requests sent 2026-06-10** (drafts in `access-request-emails.md`); awaiting
replies. Tick off and set the token env once each arrives.

- [x] EnergyVision — myevplatform@energyvision.be → `ENERGYVISION_TOKEN` — sent, awaiting reply
- [x] Tesla Belgium — spolireddi@tesla.com / aboumssimrat@tesla.com → `TESLA_TOKEN` — sent, awaiting reply
- [x] Monta — data@monta.com → `MONTA_TOKEN` — sent, awaiting reply
- [x] Road — roaming-dev@road.io — **not needed**: public file is live & ingesting (a token may add more, but the open feed works)
- [x] Eco-Movement — nap@eco-movement.com → `ECOMOVEMENT_TOKEN` — sent, awaiting reply
      (asked about OCPI access + whether price/availability are included)

## Suggested integration order

1. **EnergyVision** — real OCPI 2.1.1 today; validates the live pipeline.
2. **OCPI 2.2.1 support** → Tesla, Monta (clean OCPI, real coverage).
3. **DATEX II reader** → Eco-Movement (20 networks in one shot) + Gireve + INDIGO.
4. **Road** file adapter — opportunistic.
