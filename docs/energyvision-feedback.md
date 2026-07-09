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
| 9 | Namespace declarations repeated on every element (43% of payload) | drafted 2026-07-08, not yet sent | Open — re-verified 2026-07-09, still 43% (88,560 declarations, 5.3 MB) |
| 10 | Location on `aegi:entrance` instead of `locationReference` (interop note) | drafted 2026-07-08, not yet sent | Open — table unchanged since rev2 |

## Follow-up draft (new findings only — items already sent are omitted)

> Thanks for the fast turnaround on the location data — the revised table you
> published this morning (09:07 UTC) carries coordinates and full street
> addresses for every site, and we're now ingesting all ~3,765 charging
> points with live availability and prices. We also validated both feeds
> (including the revision) against the official DATEX II v3.7 schema set from
> docs.datex2.eu — everything checks out, confirming the compliance statement
> in your documentation.
>
> Two new findings from working with the revised feed:
>
> **1. Namespace declarations are repeated on every element.** 43% of the
> table payload — 5.3 MB of the 12.3 MB — is redundant `xmlns:`
> re-declarations of prefixes already declared on the root `d2:payload`
> element. This pattern usually comes from serializing each element
> separately and concatenating; building the document as one tree or stream
> (PHP's `DOMDocument` or `XMLWriter`) declares each prefix once at the root
> automatically. That alone shrinks both feeds by ~44% with zero semantic
> change — we verified a namespace-hoisted version still validates against
> your published XSD set and is canonically identical (exclusive-C14N
> byte-equal). Combined with gzip from our earlier mail, the 12.3 MB table
> would go over the wire at ~300 KB.
>
> **2. Interoperability note on the new location element.** The revised
> table publishes the site location on `aegi:entrance` (a `loc:PointLocation`
> with typed `addressLine`s) rather than the more common `locationReference`
> on the site. Both are schema-valid — but some consumers only read
> `locationReference`, so publishing it there as well (or documenting the
> choice explicitly) would maximise compatibility. Related detail from the
> same structure: with location now fixed, a site `aegi:name` is the last
> missing piece for a good map label — everything currently renders under the
> generic brand name.
