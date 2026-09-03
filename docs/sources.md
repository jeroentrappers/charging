# NAP charging data sources (BE + NL/DE/FR/ES/PT/FI/CH/PL/AT)

Catalogue of EV-charging feeds published on the National Access Points under
AFIR Article 20, with what each takes to consume and how to request access.
Adding a source to the running system = insert a `cpo` row (+ token env) — the
scheduler hot-reloads it.

Last researched: 2026-06-09 (BE), 2026-06-11 (NL/DE/FR expansion),
**2026-08-18 (EU-wide sweep after the 2026-04-14 DATEX II mandate)**.
Live update 2026-06-24: **Tesla**, **Group Indigo** and **Eco-Movement** are now
enabled and ingesting in production (see status notes below).
Live update 2026-08-18: **ES, PT, FI and CH** wired (all open, no key), and
**FR gained availability** from the new consolidated national dynamic file —
see [EU expansion](#eu-expansion--wired-2026-08-18).
Live update 2026-08-19: keys arrived for **PL (UDT EIPA)** and **AT (E-Control)**
— both wired. Austria is now our richest new source: ~38,600 EVSEs with live
status AND structured euro ad-hoc prices on 98% of them.
Live update 2026-09-04: **Eco-Movement moved to the Belgian NAP's own AFIR feed**
(`https://nap-be.eco-movement.com/datex2/v1/locations`, Bearer token) — DATEX II
v3 **JSON** with **live status and ad-hoc price**, replacing the old locations-only
XML export. 16,700 sites / **69,408 connectors** (was ~35,800), power on all but
two (was 40% missing), price on 56,717 of them. See [Eco-Movement (BE NAP)](#eco-movement--be-nap-2026-09).

## Beyond Belgium — wired 2026-06-11 (no paid feeds)

The same AFIR rule means each country's NAP is the backbone. Map coverage is
broadly free; **ad-hoc price is the scarce thing** (only NL is free + structured
today). Unpriced chargers map fine and show no comparable price (info-only).
See the [[eu-data-sources]] memory for the full landscape.

| Source (`cpo` id) | Country | type | Locations | Status | **Ad-hoc price** | Notes |
|---|---|---|---|---|---|---|
| **NDW · DOT-NL** (`dotnl`) | 🇳🇱 NL | `ocpi_file_gz` | ✅ ~88k | ✅ | ✅ **structured** | Open OCPI 2.2.1 .json.gz (locations+tariffs); ~226k connectors, ~50% priced, incl. Fastned. Daily poll + hourly status. |
| **Bundesnetzagentur** (`bnetza`) | 🇩🇪 DE | `bnetza` | ✅ ~134k | ❌ | ❌ | Official registry CSV (Latin-1/`;`), scraped dated URL. Location-only. Monthly. |
| **transport.data.gouv IRVE** (`irve`) | 🇫🇷 FR | `irve` | ✅ ~230k | ✅ (2026-08-18) | ❌ (free-text only) | Consolidated GeoJSON, ~585 MB, streamed, monthly. Availability now overlaid from the consolidated national **dynamic** CSV (daily) — see [EU expansion](#eu-expansion--wired-2026-08-18). |
| Eco-Movement / Chargeprice | EU | — | ✅ | ✅ | ✅ | **Commercial — deliberately NOT integrated** (no paid feeds). The all-EU priced backbone if that changes. |

DE/FR price will improve as the AFIR **DATEX II** mandate (14 Apr 2026) matures
in Mobilithek (DE) and the IRVE-dynamique feed (FR). Re-checked 2026-08-18: the
French dynamique schema still has **no price field** — only availability.

## EU expansion — wired 2026-08-18

Sweep of every national access point after the **2026-04-14 DATEX II mandate**.
Four new countries were open enough to wire the same day; two more are free but
need a registration (below). All figures are from the live feeds on 2026-08-18.

| Source (`cpo` id) | Country | type | Locations | Status | **Ad-hoc price** | Notes |
|---|---|---|---|---|---|---|
| **Fintraffic AFIR** (`fi-fintraffic`) | 🇫🇮 FI | `fintraffic` | ✅ 3,783 sites / 19,852 EVSEs | ✅ **live** | ✅ **structured** | The NAP collects the CPOs' OCPI and republishes it open as one national feed (locations + statuses + tariffs, regenerated every minute). Prices are **net of VAT** → the adapter grosses them up. |
| **Mobi.E** (`pt-mobie`) | 🇵🇹 PT | `datex_afir` | ✅ 8,204 sites / 20,285 points | ✅ **live** | ✅ **structured** | Portugal's single national network. AFIR table+status pair, open. Encodes price as `pricingPolicy` + fee per `electricEnergyMixOverride`, not `energyRateUpdate`. 187 MB table / 41 MB status. |
| **DGT · electrolineras** (`es-dgt`) | 🇪🇸 ES | `datex` | ✅ 12,354 sites / 37,050 points | ❌ | ❌ | Open DATEX II v3 (CC-BY), daily, ~85 MB. Same profile as Eco-Movement/Indigo but coordinates on `coordinatesForDisplay` and the address as labelled text lines. No status publication exists yet. |
| **SFOE ich-tanke-strom** (`ch-sfoe`) | 🇨🇭 CH | `oicp` | ✅ 14,437 public EVSEs (of 19,138) | ✅ **live** | ❌ | Outside AFIR, so **OICP (Hubject) JSON**, not DATEX/OCPI. Restricted-access + test points are dropped as non-public. Licence O-By-Ask (attribution). |
| **FR dynamic** (`irve`, extended) | 🇫🇷 FR | `irve` | (unchanged) | ✅ **new** | ❌ | The NAP now publishes a consolidated national **dynamic** CSV (114,796 rows) keyed on `id_pdc_itinerance` — the same key as the static base. Rebuilt nightly, and ~40% of rows are months stale, so rows older than `irve.DynamicMaxAge` (36h) are ignored rather than reported as free. |

**Wired 2026-08-19 (free, registration-gated keys):**

| Source (`cpo` id) | Country | type | Locations | Status | **Ad-hoc price** | Notes |
|---|---|---|---|---|---|---|
| **E-Control Ladestellen** (`at-econtrol`) | 🇦🇹 AT | `econtrol` | ✅ 14,679 stations / 38,567 EVSEs | ✅ **live** | ✅ **structured (EUR)** | 37,956 of 38,567 priced: per kWh, per minute, start fee, and a blocking fee **with a grace threshold** (mapped to PARKING_TIME + `AfterMinutes`). Key = `Apikey` header **and** a matching `Referer` — see below. |
| **UDT EIPA** (`pl-eipa`) | 🇵🇱 PL | `eipa` | ✅ ~7,100 electric stations / 14,190 connectors | ✅ **live** | ⚠️ **structured, but PLN** | 11,733 priced (median 2.19 PLN/kWh). Six proprietary JSON files, token is the **last path segment** of each URL. Prices are stored as published but get **no euro comparable price** (see below). |

**🇦🇹 Austria — what to know.** The key is bound to a DOMAIN: every request needs
`Apikey: <key>` *and* a `Referer` whose host matches the registration
(`https://charging.appmire.be`). A mismatch answers **401 with a German prose
page**, not JSON. There is no bulk export: the register is walked
`operators → stations → points` (1,113 → 14,679 → 38,567), about **15,800 small
requests per pass**, ~5.5 min at 4 concurrent. Status lives on the point payload,
so availability cannot be refreshed more cheaply — hence a daily price pass plus
**twice-daily** availability, and the operator/station tree cached for a day.
E-Control publishes no rate limit. **Two things worth asking them:** what polling
cadence they permit, and whether the DATEX II export their NAP record mentions is
available — it would replace the whole crawl.

**🇵🇱 Poland — currency handling (FX).** EIPA quotes PLN. Published components are
always stored **as published**, in their own currency; only the derived
`comparable_price_eur` (and the per-profile matrix) is euro-normalised, using the
**ECB daily reference feed** — open, no key, ~1.5 KB:
`https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml` (`FX_RATES_URL`,
`internal/fx`). Rates are units per euro, so converting divides: 2.19 PLN/kWh →
€0.507/kWh at 4.3190.
Because the PWA prices chargers client-side from the raw tariff, the API also
serves the rates on **`GET /fx`**; the app loads them once per session and
normalises before ranking (`toEUR` in `web/src/pricing.ts`).
If no rate is available — `FX_RATES_URL` empty, an unknown currency, or a rate set
older than a week (`fx.MaxAge`, since the ECB publishes only on working days) —
the tariff is stored and displayed but gets **no comparable price**, so the
charger sorts as unpriced instead of being ranked at a made-up parity. The detail
view always renders each tariff in its own currency (`ui.money`).
Download limits are enforced per account: **10/hour** for the five static files,
**240/hour** for the dynamic one, hence the 12h static cache and 10-minute status.

**Checked, nothing consumable:** 🇮🇪 IE (TII "DXP" is CPO-onboarding OCPI only),
🇩🇰 DK national NAP (login request), 🇸🇪 SE / 🇳🇴 NO (their NAP is **NOBIL** —
free API key), 🇮🇹 IT (`nap-1926.it` does not resolve; PUN publishes nothing
open). `napspan.com` resells all 24 NAPs normalised — commercial, so out under
the no-paid-feeds rule. Next-sweep leads: the NAPCORE monitor
(`https://eunapmonitoring.napcore.imet.gr/`) and the EU ITS Platform NAP section
(`https://andnet.ro/nap_eueip/index.php`).

## Which sources expose ad-hoc PRICE? (open-pricing sweep, 2026-06-10)

| Source | Open (no key)? | Ad-hoc price? | Availability? |
|---|---|---|---|
| **Road** | ✅ yes | ✅ **yes** | ✅ yes (status) |
| **Monta** (Public API) | list: ✅ open / price: ⚠️ key | ⚠️ key + **per-EVSE** only | ⚠️ key + per-EVSE |
| **EnergyVision** ✅LIVE | ⚠️ free key (email) | ✅ **yes** (status-feed rate updates) | ✅ yes (60s status feed) |
| **Tesla** ✅LIVE | ⚠️ key (pre-encoded) | ❌ **no tariffs module** | ✅ yes (live status) |
| **Eco-Movement** ✅LIVE | ⚠️ free token (email) | ✅ **yes** (since the 2026-09 NAP feed) | ✅ **yes** (same feed) |
| **INDIGO** ✅LIVE | ✅ yes | ❌ no (static) | ❌ no |
| Gireve (EVCI) | ❌ fee-based license | ❌ not in open set | — |

**Conclusion:** **Road is the only fully-open (no-credential) source with ad-hoc
price in bulk** — and it's already live. No other open feed gives bulk price:
INDIGO and **Monta's open charge-points list** are
location-only (Eco-Movement was too, until its 2026-09 NAP feed added price and
status behind a free token). **Monta's** price + availability are key-gated and **per-EVSE**
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
| **EnergyVision** ✅LIVE | 1 CPO (~1,238 sites / 3,757 charging points) | **DATEX II v3.7 AFIR** (Bearer key) | `https://datex.cpo.energyvision.be/datex/energy-infrastructure-{table,status}` | ✅ wired & ingesting (`source_type='datex_afir'`) — table + 60s status feed with **live availability AND station-level ad-hoc prices**. Coordinates + street addresses landed with their 2026-07-08 revision (on `aegi:entrance`) after our feedback. Key expires every 6 months | myevplatform@energyvision.be |
| **Tesla Belgium** ✅LIVE | 1 CPO (~92 sites / 456 connectors) | OCPI **2.2.1** | `https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/` | ✅ wired & ingesting — locations + **live status**, no tariffs module. Token is pre-base64 (`TESLA_TOKEN=base64:…`) | spolireddi@tesla.com |
| **Monta** | 1 CPO | AFIR JSON (OCPI 2.2.1) | `https://docs.partner-api.monta.com/reference/get-afir-charge-points` | ⚠️ needs 2.2.1 / adapter | data@monta.com |
| **Road** ✅LIVE | 1 CPO (~3,300 sites / 7,700 connectors) | OCPI 2.2.1 static JSON | `https://roaming.road.io/files/9ef09c78-2666-418a-aa45-4f2261e2e305/{locations,tariffs}.json` | ✅ **open, no key** — wired & ingesting (incl. prices) | roaming-dev@road.io |
| **Eco-Movement** ⭐ ✅LIVE | **the BE NAP (16,700 sites / 69,408 connectors)** | **DATEX II v3 AFIR JSON** (Bearer token) | `https://nap-be.eco-movement.com/datex2/v1/locations` | ✅ wired & ingesting — **locations + power + live status + ad-hoc price**, paginated 1,000 sites/page (~104 MB per pass). Token set in DB via `chargingctl sources set-token ecomovement …` | support@eco-movement.com |
| **Gireve (EVCI)** | many (roaming) | DATEX II XML | dataset `/en/dataset/evci` | ❌ needs DATEX II reader | via dataset page |
| **Group INDIGO** ✅LIVE | 1 CPO (~2,300 connectors) | DATEX II XML (open) | `…/resource/d4bc8ddd-…/download/indigo-data-evcharging-static-datexii.xml` | ✅ wired & ingesting — **location-only** (no price/status), open, no key | via dataset page |

⭐ **Eco-Movement is the highest-leverage source**: one integration covers
Allego, bp pulse, Blink Charging, ChargePoint, Circle K, Dats24, Electra,
Fastned, Gabriels, Interparking, IONITY, Lidl, Litran, Porsche, PowerGo, Shell
Recharge, Sparki, Q8 electric, TotalEnergies. Its NAP feed is **DATEX II** and
the static set may be locations + AFIR specs only — confirm whether **ad-hoc
price + dynamic availability** are included (we need both for history). Their
commercial **OCPI** API is the richer alternative.

## What's needed to consume each format

- **OCPI 2.1.1** — ✅ supported (no live source; EnergyVision moved to DATEX II).
- **OCPI 2.2.1** — ✅ supported: `/versions` discovery, base64 `Token` auth, and
  2.2.1 fields (`max_electric_power`, `tariff_ids`). **Tesla is live** (locations +
  live status; it advertises no tariffs module, so the engine skips the tariffs
  fetch via `Client.HasModule`). A `base64:` token prefix sends a pre-encoded
  credential verbatim. Monta uses its own adapter.
- **DATEX II** — ✅ reader built (`internal/datex`, v3 EnergyInfrastructure),
  wired via `cpo.source_type='datex'`. **Live: Group Indigo (~2,300 connectors)**
  — coverage only (no ad-hoc price, no live status), so enable it for reach, not
  for price comparison. Some NAP feeds of this shape authenticate with a `?token=`
  query param (folded in by `feedFor`) rather than a Bearer header. Mandatory NAP
  format from 2026-04-14.
- **DATEX II AFIR JSON, paginated (`source_type='ecomovement'`)** — ✅ the Belgian
  NAP feed: Bearer token, 1,000 sites per page, each page carrying the table AND
  the status publication for its own sites. Parsed by the shared AFIR JSON reader
  (`internal/datex`) and walked by `internal/ecomovement`.
- **DATEX II AFIR pair (Bearer)** — ✅ (`source_type='datex_afir'`): a
  `<table>|<status>` URL pair authenticated with `Authorization: Bearer <key>`,
  parsed by the shared AFIR reader (station-level `energyRateUpdate` supported).
  The table is cached in-process for 1h so the 5-minute status polls don't
  re-download it. **EnergyVision** uses this.
- **Static JSON file** (Road) — ✅ **done** (`source_type='ocpi_file'`): fetches
  `{base}/locations.json` + `{base}/tariffs.json` (bare OCPI arrays). It's **open
  (no token)** and carries real ad-hoc prices, so it's enabled by default and
  ingesting today — the live proof of the whole pipeline before any key arrives.
  (The file's UUID path may rotate; update via `chargingctl sources add road --url …`.)

Live sources: `road`, `dotnl`, `bnetza`, `irve`, `monta`, **`tesla`** (OCPI 2.2.1),
**`indigo`** + **`ecomovement`** + **`es-dgt`** (DATEX II), **`energyvision`** +
**`pt-mobie`** (DATEX II AFIR pair — live availability + prices),
**`fi-fintraffic`** (national OCPI-shaped JSON), **`ch-sfoe`** (OICP),
**`pl-eipa`** (UDT JSON files, PLN prices), **`at-econtrol`** (keyed REST crawl).

## Access-request checklist

Each direct CPO needs its own free key (AFIR: non-discriminatory, no cost).
**All requests sent 2026-06-10** (drafts in `access-request-emails.md`); awaiting
replies. Tick off and set the token env once each arrives.

- [x] EnergyVision — myevplatform@energyvision.be → `ENERGYVISION_TOKEN` — ✅ **docs + API key received 2026-07-08** (DATEX II v3.7, not OCPI). Wired, enabled & ingesting (~3,765 points, live status + prices; they added the missing coordinates on 2026-07-08 after our feedback). NOTE: key expires every 6 months — re-request by email.
- [x] Tesla Belgium — spolireddi@tesla.com / aboumssimrat@tesla.com → `TESLA_TOKEN` — ✅ **token received & live** (transportdata.be NAP; pre-base64, set as `TESLA_TOKEN=base64:…`)
- [x] Monta — data@monta.com → `MONTA_TOKEN` — sent, awaiting reply
- [x] Road — roaming-dev@road.io — **not needed**: public file is live & ingesting (a token may add more, but the open feed works)
- [x] Eco-Movement — nap@eco-movement.com → `ECOMOVEMENT_TOKEN` — ✅ **token received & live**. Re-issued 2026-09-03 for the new **BE NAP** interface (`nap-be.eco-movement.com`, `Authorization: Bearer <token>`); set in the DB via `chargingctl sources set-token ecomovement …`. Further tokens: support@eco-movement.com, docs at https://developers.eco-movement.com/v5/docs/belgium-datex-ii
- [x] Group Indigo — transportdata.be open dataset — **not needed**: open static DATEX II, live & ingesting
- [x] ES DGT / PT Mobi.E / FI Fintraffic / CH SFOE — **not needed**: all four are open, no key, live & ingesting since 2026-08-18
- [x] **AT E-Control** — registered at `https://admin.ladestellen.at/#/api/registrieren`; key received 2026-08-19 → `ECONTROL_APIKEY`. Live: 38,567 EVSEs with status + euro prices. NOTE: the key is tied to the `charging.appmire.be` Referer.
- [x] **PL UDT (EIPA)** — registered at `https://eipa.udt.gov.pl/reader/register`; token received 2026-08-19 → `EIPA_TOKEN`. Live: 14,190 connectors with status + PLN prices (not euro-comparable yet).

## Eco-Movement — BE NAP (2026-09)

Eco-Movement replaced its `api.eco-movement.com` DATEX II XML export with the
Belgian NAP publication at `https://nap-be.eco-movement.com/datex2/v1/`. Same
aggregator, an entirely different interface — the old URL now answers
`{"status_code":3001,"status_message":"Interface not found"}`.

| | old XML export | new NAP feed |
|---|---|---|
| Format | DATEX II XML | DATEX II v3 **JSON**, AFIR profile 01-00-00 |
| Auth | `?token=` in the URL | `Authorization: Bearer <token>` |
| Size | one ~31 MB document | 17 pages × ~6 MB (`?limit=1000&offset=…`, `Link: rel="next"`) |
| Connectors | ~35,800 | **69,408** (16,700 sites) |
| Power | **40% missing** | 2 missing |
| Live status | ❌ | ✅ six values, on every point |
| Ad-hoc price | ❌ | ✅ 56,717 points (81.7%) |

Feed specifics the readers handle:

- **Both publications per page.** Each page is one document holding the
  `aegiEnergyInfrastructureTablePublication` *and* the
  `aegiEnergyInfrastructureStatusPublication` for exactly its sites, at the
  document ROOT (no `messageContainer`/`payload` envelope). There is no bulk
  status endpoint — the only other route is `/status/{evse_id}`, one request per
  EVSE — so a status pass is a full walk of the feed (~104 MB, ~1 min,
  uncompressed). Hence hourly availability, daily price.
- **Location on the site's `entrance`**, not `locationReference`, with the
  address under `locLocationExtensionG > FacilityLocation`.
- **CPO on `energyDistributor`**, not `operator`.
- **Net prices.** Every `energyPrice` is `taxIncluded:false` with `taxRate`
  alongside — and the rate's unit is inconsistent (`21` on most, `0.21` on some
  of the same 21% VAT). The reader grosses up, treating a rate above 1 as a
  percentage. Median ad-hoc price after gross-up: €0.5348/kWh.
- **Identity.** Refill points are keyed by an internal `idG`
  (`BE-ENE-EENECO_G44971-1`) with the roaming eMI3 id on the connector's
  `externalIdentifier` (`BE*ENE*EENECO*G44971*1`). We key chargers by the roaming
  id: it matches 99.6% of what the old export published, so the switch keeps
  chargers' identity (and their price history) instead of duplicating them, and
  it is the id every other Belgian source uses.

## Suggested integration order

1. **EnergyVision** — real OCPI 2.1.1 today; validates the live pipeline.
2. **OCPI 2.2.1 support** → Tesla, Monta (clean OCPI, real coverage).
3. **DATEX II reader** → Eco-Movement (20 networks in one shot) + Gireve + INDIGO.
4. **Road** file adapter — opportunistic.
