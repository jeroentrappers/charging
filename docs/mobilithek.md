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

The pull URLs look like:
```
https://mobilithek.info:8443/mobilithek/api/v1.0/subscription/datexv3?subscriptionID=<ID>
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

## Reality check

Coverage was ~50% of German charging capacity in late 2025 and **ad-hoc price is
the weakest-populated field**; the DATEX II mandate only becomes binding
**14 Apr 2026**, so expect partial/uneven price + many `free`/`other` price
types until then. The parser is unit-tested against crafted DATEX II v3 samples;
the **first real pull may surface an element-path quirk** to tweak, since we
can't test against the live (cert-gated) feed without your subscription.
