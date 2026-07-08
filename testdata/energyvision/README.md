# EnergyVision DATEX II feed snapshots

Raw responses from `https://datex.cpo.energyvision.be/datex/*` (Bearer-auth),
kept as evidence of what the publisher served on each date and as realistic
fixtures for the AFIR parser. Gzipped verbatim — `gunzip -k <file>` to inspect.

## Timeline

| File | Feed `publicationTime` | Fetched (CEST) | What it shows |
|---|---|---|---|
| `table-v1-2026-07-07T174533Z.xml.gz` | 2026-07-07T17:45:33Z | 2026-07-08 08:41 | **v1 table: NO location data at all.** 1,238 sites / 3,757 refill points with connector+power, but zero `latitude`/`locationReference`/address/name/EVSE-id occurrences in the whole 8.3 MB document. Reported to EnergyVision that morning. |
| `status-2026-07-08T064201Z.xml.gz` | 2026-07-08T06:42:01Z | 2026-07-08 08:42 | Status feed: per-point availability + station-level `energyRateUpdate` prices (3,407 points priced). Joins 100% to the table by refill-point id. |
| `table-rev2-2026-07-08T090719Z.xml.gz` | 2026-07-08T09:07:19Z | 2026-07-08 11:07 | **Revised table, published hours after our feedback:** coordinates + street addresses + city/postcode now present for every site — on `aegi:entrance` (a `loc:PointLocation` with typed `addressLine`s), not `locationReference`. Still no site names or eMI3 EVSE ids. 12.3 MB. |
| `table-v1.response-headers.txt` | — | 2026-07-08 08:41 | v1 response headers: `Cache-Control: max-age=0, private`, no ETag/Last-Modified, no gzip, `PHPSESSID` cookie — the transport-level feedback items. |
| `health-2026-07-08.xml` | — | 2026-07-08 08:42 | `/datex/health` sample. |

## SHA-256 of the raw (uncompressed) documents

```
a6bf16d224af529640228304d712f0a62f1d0df913fc1f2e5d10a68b1e323afe  table-v1 (8,287,344 bytes)
4f946d49388a755e034ea1359e84a6c2146197d6dca2e62221399053bfce5e3f  status   (5,104,739 bytes)
113a4e412c33c70d48479516b2d74207bc59faf1a769bb4a5f11cb0dd75edf29  table-rev2 (12,279,182 bytes)
```

Both table snapshots and the status snapshot validate against the official
DATEX II v3.7 schema set (`make validate-datex FEEDS=<file>`); the v1 table was
schema-valid yet unusable — the schema marks location optional, AFIR content
requirements don't.
