# Belgian NAP charging data sources

Catalogue of EV-charging feeds published on Belgium's National Access Point
([transportdata.be](https://www.transportdata.be/)) under AFIR Article 20, with
what each takes to consume and how to request access. Adding a source to the
running system = insert a `cpo` row (+ token env) — the scheduler hot-reloads it.

Last researched: 2026-06-09.

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
