# Open bulk dataset export

We're built on open AFIR / [transportdata.be](https://transportdata.be) data, so
we re-publish our normalized, price-enriched dataset as **open static dumps** —
the same pattern ROAD uses for its rotating OCPI export. This lets anyone grab
the whole dataset cheaply (and keeps "give me everything" traffic off the live
query API).

All files live under **`/export`** and are regenerated on a schedule:

| File | Format | Refreshed | Contents |
|------|--------|-----------|----------|
| `/export/index.json` | JSON manifest | on every refresh | generated-at, next-refresh, counts, per-file size + sha256, licence |
| `/export/chargers.ndjson` | NDJSON (`application/x-ndjson`) | full (~5 min) | one normalized connector per line: location, power, plug, CPO, availability, comparable session prices, full structured `tariff` |
| `/export/chargers.geojson` | GeoJSON (`application/geo+json`) | full (~5 min) | `FeatureCollection` of charger points for mapping |
| `/export/ocpi/locations.json` | OCPI 2.1.1 Locations | full (~5 min) | EVSEs/connectors grouped per location, each connector referencing `tariff_ids` |
| `/export/ocpi/tariffs.json` | OCPI 2.1.1 Tariffs | full (~5 min) | de-duplicated tariffs referenced by the locations dump |
| `/export/availability.json` | JSON | delta (~1 min) | live status + `available_count` per charger id — small and frequently rotated |

Prices change rarely, so the full dump rotates every few minutes; availability
rotates faster in its own small file. The split mirrors the internal ingestion
hybrid (slow price, fast status).

## Consuming

- **Start at the manifest.** `index.json` gives `generated_at`,
  `next_full_refresh` / `next_availability_refresh`, byte sizes and `sha256` for
  each file, so you can poll efficiently and verify integrity.
- Files are served with `Cache-Control: public, max-age=30`, `Last-Modified`,
  and gzip (`Accept-Encoding: gzip`) — conditional `If-Modified-Since` requests
  return `304`. A CDN in front absorbs the load.
- Writes are **atomic** (temp file + rename), so a fetch never sees a partial
  file. To correlate availability with the full dump, read `availability.json`
  and join on charger `id`.

### Size

The dump is small. With Road alone (~7.7k connectors) the full NDJSON is a few
MB (~1–2 MB gzipped); with every source enabled (~40–50k connectors) expect
~60–90 MB raw / ~6–10 MB gzipped — still a single static file.

## Licence & attribution

The dataset is derived from open EV charging data published via the Belgian
National Access Point (transportdata.be) under AFIR Article 20, re-published
under **ODbL-1.0**; © the respective charge point operators. The exact strings
are carried in `index.json` (`license`, `attribution`).

## Configuration

| Env var | Default | Meaning |
|---------|---------|---------|
| `EXPORT_DIR` | `./export` | Directory the API writes/serves dumps from. **Empty disables the export entirely.** |
| `EXPORT_FULL_EVERY` | `5m` | Full snapshot (NDJSON/GeoJSON/OCPI) cadence |
| `EXPORT_AVAIL_EVERY` | `1m` | Availability-delta cadence |

The snapshotter runs inside the `api` process (it already holds the DB pool and
serves HTTP). In production, point `EXPORT_DIR` at a mounted volume if you want
the dumps to survive a restart; otherwise they're simply regenerated on boot.
