# EnergyVision DATEX II feedback — findings register

Consumer findings on `https://datex.cpo.energyvision.be/datex/*` (docs v1,
2026-06-30). Evidence snapshots with SHA-256 manifest live in
`testdata/energyvision/`.

## Status of findings

Re-verified 2026-07-09 against a fresh fetch of both feeds. The table is
**byte-for-byte identical** to the 2026-07-08 rev2 snapshot (same SHA-256:
`113a4e41…`) — no new table has been generated since, consistent with its
24h server cache and `/metadata` still reporting `generatedAt` 2026-07-08T09:14Z.
The status feed is a fresh generation (different content, same shape) with no
structural changes. Nothing below has moved since yesterday.

| # | Finding | Sent | Status |
|---|---|---|---|
| 1 | Table missing coordinates/names/addresses | 2026-07-08 am | ✅ **Fixed same day** (rev2 table 09:07Z: coordinates + street addresses on `aegi:entrance`). Names still missing → folded into #2. |
| 2 | No eMI3 EVSE identifiers (`externalIdentifier` absent) | 2026-07-08 am | Open — re-verified 2026-07-09, still 0 occurrences |
| 3 | Dangling `energyRateReference` ids; no `applicableCurrency` in updates; prices only at station level | 2026-07-08 am | Open — re-verified 2026-07-09: 1,151 rate references, 0 resolve against the table; 0 currency elements across 2,302 price entries |
| 4 | Cache headers contradict docs (`max-age=0, private`, no ETag/Last-Modified) | 2026-07-08 am | Open — re-verified 2026-07-09, headers unchanged |
| 5 | No gzip support | 2026-07-08 am | Open — re-verified 2026-07-09, `Accept-Encoding: gzip` still ignored |
| 6 | `PHPSESSID` cookie + web-app CSP headers on API responses | 2026-07-08 am | Open — re-verified 2026-07-09, still present |
| 7 | Key expiry not observable (6-month manual rotation) | 2026-07-08 am | Open — `/metadata` still carries no expiry field |
| 8 | ~23% of points report `unknown` status | 2026-07-08 am | Open — 924/3,765 (24.5%) on 2026-07-09's status feed, essentially unchanged |
| 9 | Namespace declarations repeated on every element (43% of payload) | 2026-07-08 follow-up | Open — re-verified 2026-07-09, still 43% (88,560 declarations, 5.3 MB) |
| 10 | Location on `aegi:entrance` instead of `locationReference` (interop note) | 2026-07-08 follow-up | Open — table unchanged since rev2 |

All 10 findings have been sent (items 1–8 in the 2026-07-08 morning email,
items 9–10 in the same-day follow-up). Only item 1 has been acted on so far.
