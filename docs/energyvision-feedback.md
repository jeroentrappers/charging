# Feedback for EnergyVision — DATEX II feeds (updated 2026-07-08)

Consumer feedback on `https://datex.cpo.energyvision.be/datex/*` (docs v1,
2026-06-30). First sent 2026-07-08 morning; updated the same day after their
revised table (publicationTime 2026-07-08T09:07Z) landed. Evidence snapshots
with SHA-256 manifest live in `testdata/energyvision/`.

---

Thanks for the quick turnaround on the location data — the revised table
published this morning added coordinates and full street addresses for every
site within hours of our report, and we're now ingesting all ~3,765 charging
points with live availability and prices. The platform basics are solid:
clean DATEX II v3.7 payloads that validate against the official schema set,
stable IDs that join 100% between table and status, and genuinely useful
metadata/health/schema endpoints.

Remaining findings, in priority order:

**1. No public EVSE identifiers.** Your documentation promises "EVSE /
charging point identifiers", but refill points still carry only internal
UUIDs — no eMI3 IDs (e.g. `BE*EVI*E…`) in `externalIdentifier`. Without them,
consumers can't cross-reference your points with eMSP apps, roaming
platforms, or other NAP datasets, and AFIR's data set expects the public
identifier.

**2. No site names.** Sites have a brand ("EnergyVision") but no
`aegi:name`, so every consumer renders the same generic label. A short
human-readable name per site (as in your own app) would improve every
downstream map.

**3. Dangling rate references, and no currency.** Status-feed
`energyRateUpdate` elements reference EnergyRate ids
(`…-station-adhoc-rate`, targetClass `aegi:EnergyRate`) that don't exist in
the table publication (the table carries no `energyRate` at all), and none of
the ~1,150 updates carries `applicableCurrency` — consumers must assume EUR.
Publishing the base `energyRate` in the table and the currency in updates
would make the pricing self-contained. Note the updates sit at station level,
so per-point pricing isn't expressible in the current structure.

**4. Cache headers contradict the documented TTLs.** The docs say feeds
"return cache headers for clients", but responses send
`Cache-Control: max-age=0, must-revalidate, private` with an already-expired
`Expires`, and no `ETag`/`Last-Modified` — so conditional GETs (304) aren't
possible and every poll re-downloads the full payload. With the status feed
polled every 60 s by design, validators would save real bandwidth on both
sides whenever a regeneration produced identical content.

**5. No compression.** `Accept-Encoding: gzip` is ignored. The revised table
is now 12.3 MB and gzips to ~310 KB (−97%); the 5 MB status feed compresses
similarly. This is the single highest-leverage transport fix.

**6. Namespace declarations are repeated on every element.** 43% of the
table payload — 5.3 MB of the 12.3 MB — is redundant `xmlns:` re-declarations
of prefixes already declared on the root `d2:payload`. Declaring each prefix
once at the root (any tree- or stream-based XML writer such as PHP's
`DOMDocument`/`XMLWriter` does this automatically) shrinks both feeds ~44%
with zero semantic change; we verified a namespace-hoisted version still
validates against your published XSD set and is canonically identical
(exclusive C14N byte-equal).

**7. API responses set a PHP session cookie** (`PHPSESSID`) and carry the
web-app's browser CSP headers (Stripe, Google Fonts…). Harmless, but session
state on a machine-to-machine feed is unnecessary, defeats intermediary
caching, and suggests the feeds share the consumer web-app pipeline.

**8. Key expiry isn't observable.** Clients are required to monitor their
key's expiration date, but it isn't exposed anywhere (e.g. in `/metadata` or
a response header). Exposing it — or supporting self-service renewal — would
make the 6-month rotation manageable.

**9. Data quality: ~23% of points report `unknown` status** (879 of 3,765 in
today's feed, alongside 1,975 available / 865 charging / 46 out of order).
Is that genuinely unknown hardware state, or a gap in the feed generation for
a subset of the fleet?

One observation, not a defect: the revised table publishes the site location
on `aegi:entrance` (a `loc:PointLocation` with typed `addressLine`s) rather
than the more common `locationReference` on the site. Both are schema-valid;
just be aware some consumers only read `locationReference`, so publishing it
there as well (or documenting the choice) would maximise interoperability.
