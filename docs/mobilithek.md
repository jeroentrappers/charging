# DE Mobilithek — live availability + ad-hoc price (AFIR DATEX II)

Germany's dynamic charging data (live status **and** ad-hoc price) is published
to the **Mobilithek** National Access Point as **DATEX II** (the AFIR Recharging
profile). Unlike the Bundesnetzagentur registry (static, location-only), this
carries price + availability — but it's **gated**: mutual-TLS with an
organisation-issued client certificate, and you subscribe per CPO/aggregator
offering. We've built the consumer; the credentials are yours to provision.

## What you do (one-time)

1. **Register your organisation** at [mobilithek.info](https://mobilithek.info)
   (org account + an administrator role).
2. The org admin **requests a machine certificate** → you receive a `.p12` by
   email and the signing password by SMS. Convert to PEM (cert + key) and grab
   the Mobilithek CA/truststore cert.
3. Browse the metadata catalogue and **subscribe** to the relevant charging
   offerings. Each offering gives a **subscription ID**; AFIR providers publish a
   **static** offer (locations + ad-hoc price) and a **dynamic/status** offer
   (availability + price updates). Note both IDs per CPO/aggregator.

The M2M consumer-pull URL (from the per-subscription OpenAPI spec Mobilithek
generates — **host is `m2m.mobilithek.info`, response is gzipped JSON**; the
`mobilithek.info:8443/.../datexv3` form is wrong and 400s):
```
https://m2m.mobilithek.info/mobilithek/api/v1.0/subscription?subscriptionId=<ID>
```
Required header `Accept-Encoding: gzip`. PUSH-mode subscriptions are pullable too.

## Static-reconcile pull (backstop for push)

Push delivery is delta-based and can drop the **static** (locations + ad-hoc
price) snapshot — a source then receives only status deltas and has **zero
chargers** to attach them to. So even for push sources, set the source's
`pull_static_id` (the static subscription id) and the ingester pulls that static
on `MOBILITHEK_PULL_EVERY` (default 24h, staggered by `MOBILITHEK_PULL_STAGGER`),
ingesting it via the same path as a push. Status is left to push. Enable per
source with SQL:
```
UPDATE cpo SET pull_static_id='<static-subscription-id>' WHERE id='mob-<slug>';
```

## What you configure

Mount the PEM files into the api + ingest containers and point the env at them
(see `.env.example`):
```
MOBILITHEK_CERT_FILE=/secrets/mobilithek-cert.pem
MOBILITHEK_KEY_FILE=/secrets/mobilithek-key.pem
MOBILITHEK_CA_FILE=/secrets/mobilithek-ca.pem      # optional
```
Then add one source per offering (static + status URL joined by `|`):
```
chargingctl sources add de-allego --type mobilithek \
  --url "https://mobilithek.info:8443/.../subscription/datexv3?subscriptionID=<STATIC_ID>|https://mobilithek.info:8443/.../subscription/datexv3?subscriptionID=<STATUS_ID>"
chargingctl sources enable de-allego
```
Repeat per CPO/aggregator (offerings aren't a single national feed). The
scheduler hot-reloads new sources; set sensible crons (price daily, status more
often).

## How it works

`source_type=mobilithek` → `mobilithekFeed` (mutual-TLS, gzip) → the static
publication is parsed by `datex.ParseAFIRStatic` (connectors + ad-hoc tariff
from `EnergyRate`/`EnergyPrice`: `pricePerKWh`→ENERGY, `pricePerMinute`→TIME,
`flatRate`/`basePrice`→FLAT), and the status publication by
`datex.ParseAFIRStatus` (availability + live price updates), merged by refill-
point id. From there it flows through the normal SCD2 tariff pipeline like every
other source.

## Consumer push (and non-DATEX overlays)

Some offerings are delivered by **consumer push** instead of pull: the provider
POSTs to our Mobilithek push endpoint, which spools each packet durably
(`incoming/` → `processing/` → drop on success, else `failed/` with a `.reason`
sidecar) and ingests via `IngestMobilithekPush`. AFIR DATEX II (XML or JSON) is
parsed by `datex.ParseAFIR`.

Not every pushed feed is DATEX II. **eliso** pushes a flat, non-DATEX JSON
overlay — `{"evses":[{evseId, adhoc_price, blocking_fee, operational_status,
availability_status, …}]}` — carrying live availability + ad-hoc price but **no
locations**. `IngestMobilithekPush` detects this shape first (`parseElisoPush`)
and applies it as a status + price overlay, matched by `evseId` alone
(`ChargersForEVSEAny`) because the locations are seeded under a broker/aggregator
CPO, not a per-operator one. Statuses map operational/availability →
AVAILABLE/CHARGING/OUTOFORDER/UNKNOWN; `adhoc_price`→ENERGY (the comparable),
`blocking_fee`→PARKING_TIME (display only, excluded from the comparable). EVSEs
we have no location for are skipped (the push reports rows=0). A payload that
matches neither eliso nor a known AFIR publication is quarantined to `failed/`.

## Delta feeds + the source heartbeat

Most status pushes are **deltas**: a publisher sends only the refill points whose
status/price just changed (Tesla pushes one at a time, EnBW small batches), not
its whole fleet. `UpsertStatus` bumps `charger_status.updated_at` only for the
chargers in the push, so an unchanged-but-healthy charger's reading ages out and
would read as **stale** (default `AVAILABILITY_STALE_AFTER=15m`) — dropping from
"available now" results — even while its source is actively pushing siblings.

To avoid that, every parsed push bumps `cpo.last_push_at` (a per-source
heartbeat, `BumpCPOPush`). The read-path freshness check treats a charger as
live when it has a real status reading **and** (its own update is recent **or**
its source pushed recently). So a quiet stall stays visible while its source is
demonstrably connected, and only goes stale once the *source* itself falls
silent. Snapshot feeds (eliso) and pull sources refresh every charger each cycle,
so they don't rely on the heartbeat.

## Reality check

Coverage was ~50% of German charging capacity in late 2025 and **ad-hoc price is
the weakest-populated field**; the DATEX II mandate only becomes binding
**14 Apr 2026**, so expect partial/uneven price + many `free`/`other` price
types until then. The parser is unit-tested against crafted DATEX II v3 samples;
the **first real pull may surface an element-path quirk** to tweak, since we
can't test against the live (cert-gated) feed without your subscription.
