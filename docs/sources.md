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
| **Road** | 1 CPO | OCPI-shaped JSON file | `https://roaming.road.io/files/9ef09c78-2666-418a-aa45-4f2261e2e305/locations.json` | ⚠️ static file, needs adapter | roaming-dev@road.io |
| **Eco-Movement** ⭐ | **20 networks** | **DATEX II** XML | `https://api.eco-movement.com/api/nap/datexii/locations?token=…` (token in URL, open) | ❌ needs DATEX II reader | nap@eco-movement.com |
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

- **OCPI 2.1.1** — works with the current client today (EnergyVision).
- **OCPI 2.2.1** — needs: the `/versions` discovery handshake, the 2.2.1 auth
  scheme (`Authorization: Token base64(token)`), and 2.2.1 tariff fields (VAT).
  Unlocks Tesla + Monta. *Build this next to scale OCPI sources.*
- **DATEX II** — needs a DATEX II `EnergyInfrastructure` reader mapping into our
  canonical model. Unlocks Eco-Movement (20 networks), Gireve, INDIGO. Mandatory
  NAP format from 2026-04-14, so this is a strategic build, not optional.
- **Static JSON file** (Road) — small adapter to fetch a file URL instead of an
  OCPI module; low effort, low coverage.

## Access-request checklist

Each direct CPO needs its own free key (AFIR: non-discriminatory, no cost).
Send the same short request (use-case: public price-comparison app; ask for
OCPI Locations + Tariffs access and the token):

- [ ] EnergyVision — myevplatform@energyvision.be  → `ENERGYVISION_TOKEN`
- [ ] Tesla Belgium — spolireddi@tesla.com / aboumssimrat@tesla.com → `TESLA_TOKEN`
- [ ] Monta — data@monta.com → `MONTA_TOKEN`
- [ ] Road — roaming-dev@road.io → `ROAD_TOKEN`
- [ ] Eco-Movement — nap@eco-movement.com (NAP DATEX token may be public; ask about
      OCPI access + whether price/availability are included) → `ECOMOVEMENT_TOKEN`

## Suggested integration order

1. **EnergyVision** — real OCPI 2.1.1 today; validates the live pipeline.
2. **OCPI 2.2.1 support** → Tesla, Monta (clean OCPI, real coverage).
3. **DATEX II reader** → Eco-Movement (20 networks in one shot) + Gireve + INDIGO.
4. **Road** file adapter — opportunistic.
