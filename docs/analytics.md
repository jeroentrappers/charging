# Analytics (first-party, server-side)

We collect analytics **server-side, first-party** — no third-party scripts, no
cookies, and therefore no consent banner. This suits the app because it's
API-centric (most user intent crosses the Go API) and lets us also see feed/API
consumers that a browser-only tool never would. Operational metrics stay in
Prometheus (`/metrics`); this is for **product usage** and **feed consumers**.

## How it works

- **Recorder** (`internal/analytics`): a non-blocking, bounded buffer drained by a
  background goroutine that batch-inserts into `analytics_event`. If the buffer
  fills, events are **dropped** (counted + logged) — it never blocks or fails a
  request.
- **API capture** (`recordAPI` middleware): one event per matched route, using
  the chi **route template** (`/chargers/{id}`, never a raw id) so there's no PII
  or cardinality blow-up. Maps routes to product events (`search.cheapest`,
  `charger.view`, `explorer.list`, …).
- **Feed capture** (`recordFeed` on `/export`): `feed.pull` per served file with
  `{format}` (datex-xml/json, ndjson, geojson, ocpi, …) — consumer/integrator
  analytics.
- **Client events** (`POST /events`): first-party, rate-limited, same-origin, for
  interactions the server can't see (map moves, PWA install, session-price
  slider). Recorded under a `client.` prefix.

## Privacy

No raw PII is stored. The visitor key is `sha256(ANALYTICS_SALT | date | client_id | ip)`
truncated — a **daily-rotated, salted hash**. It supports per-day unique-visitor
and per-consumer counts without storing or being able to reverse an IP. Because
there are no cookies and no third parties, no GDPR consent banner is required.
`ANALYTICS_SALT` keeps the hash stable across restarts (else a per-process random
salt is used).

## Reading it

Admin rollup (bearer `ADMIN_TOKEN`):

```
GET /admin/analytics?days=7
```

Returns totals, unique visitors, top events, top endpoints, feed consumers,
downloads-by-format, and events-per-day. Backed by `store.Analytics`, which reads
the `analytics_event` table directly — extend with new rollups as needed.
