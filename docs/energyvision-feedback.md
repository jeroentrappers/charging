# EnergyVision DATEX II feedback — findings register

Consumer findings on `https://datex.cpo.energyvision.be/datex/*` (docs v1,
2026-06-30). Evidence snapshots with SHA-256 manifest live in
`testdata/energyvision/`.

## Status of findings

**Re-verified 2026-07-09 (PM)** against a fresh fetch of both live feeds, after
EnergyVision emailed that they'd "handled most" comments (GZIP "still in
progress"). They shipped a **major revision**: a newly generated table
(`/metadata generatedAt` now `2026-07-09T12:06Z`, was stuck at 07-08T09:14Z),
**7 of 10 findings resolved**, and — as flagged in the email — **non-operational
stations removed**: the table dropped from **3,765 → 3,073 refill points**.
Contrary to the "in progress" note, **GZIP is already being served** on both
feeds. Evidence counts below are from the 12:06Z table + concurrent status feed.

On our side, we reloaded EnergyVision (full ingest) and **soft-retired the 692
de-listed chargers**, so they no longer appear in our feeds or app; ingest now
auto-retires stations a full-snapshot source stops publishing.

| # | Finding | Status (2026-07-09 PM) |
|---|---|---|
| 1 | Table missing coordinates/names/addresses | ✅ **Fixed** (coordinates + street addresses present). Names still absent — sites carry `brand` (1,075) but no `name` element. |
| 2 | No eMI3 EVSE identifiers (`externalIdentifier` absent) | ✅ **Fixed** — `externalIdentifier` now present (6,146 across 3,073 refill points; was 0). |
| 3 | Dangling `energyRateReference` ids; no `applicableCurrency`; station-level prices | ✅ **Largely fixed** — references now resolve: **1,072/1,073** status refs match a table `energyRate` id (was 0/1,151), and the table carries `applicableCurrency` (2,144). Residual: status *updates* still omit `applicableCurrency` (0), but it's now resolvable via the valid reference. |
| 4 | Cache headers contradict docs (`max-age=0, private`, no ETag) | ✅ **Fixed** — `Cache-Control: public, max-age=86400` (table) / `max-age=60` (status) + weak `ETag` on both. (`Last-Modified` still absent; ETag suffices.) |
| 5 | No gzip support | ✅ **Fixed** — `Content-Encoding: gzip` served on both feeds (table 8.0 MB raw → **448 KB** gzipped; status 2.42 MB → **177 KB**). Email said "in progress"; observed working. |
| 6 | `PHPSESSID` cookie + web-app CSP headers on API responses | ✅ **Fixed** — no `Set-Cookie`, no CSP header on the DATEX responses. |
| 7 | Key expiry not observable (6-month manual rotation) | ⏳ **Open** — `/metadata` still carries no expiry field (only `updateFrequencySeconds` + contact). |
| 8 | ~23% of points report `unknown` status | 🔸 **Much improved** — **231/3,073 (7.5%)** unknown, down from 924/3,765 (24.5%). Not zero. |
| 9 | Namespace declarations repeated on every element (43% of payload) | ✅ **Fixed** — namespaces declared once at the root (7 decls table / 5 status; was 88,560). Payload ~26 MB → 8.0 MB. |
| 10 | Location on `aegi:entrance` instead of `locationReference` (interop note) | ⏳ **Open** — still on `aegi:entrance` (2,150; `locationReference` 0). Low priority. |

**Score: 7 fixed (#1,2,3,4,5,6,9), 1 much-improved (#8), 2 open (#7 key expiry,
#10 entrance location).** Both open items are minor; #7 is operational, #10 is an
interop nicety. All 10 were sent 2026-07-08 (items 1–8 morning, 9–10 follow-up).

## New finding (2026-07-09) — implausible `pricePerMinute` values

| # | Finding | Status |
|---|---------|--------|
| 11 | **Implausible time-based prices.** The status feed's `energyRateUpdate` carries `priceType=pricePerMinute` with values that are impossible as genuine per-minute charging rates: observed **1.04 (×306), 3.60 (×2), 19.80 (×48)** €/min. Per DATEX AFIR these are €/minute, i.e. €62.40 / €216 / **€1188 per hour** — the €19.80 value alone drives charger `41360802` (Blankenbergse Steenweg 10) to a comparable session price of ~€512. The magnitudes look like **hourly time-fees mislabelled as per-minute** (€19.80/h, €3.60/h, €1.04/h are all plausible idle/time fees), but this needs confirmation from EnergyVision. | 🔴 **Open.** Re-checked against a live fetch of the status feed **2026-07-09 (PM)** after they said they'd look "asap" — **not yet fixed**: feed still carries `pricePerMinute` 1.04 (×206), 3.60 (×2), 19.80 (×50); charger `41360802` still shows €512 / `TIME=1188`. Awaiting their unit confirmation/correction. |

Evidence: `testdata/energyvision/status-2026-07-08T064201Z.xml.gz` (distinct priceType/value
counts) and the live `/api/chargers/41360802` (`TIME` component = 1188 €/h).

Our side: because our comparable-session price multiplies the TIME component by the
session duration, one bad per-minute value dominates the headline price and pollutes
ranking/insights. Decision (2026-07-09): treat this as a source-data issue and wait
for EnergyVision to confirm/correct, rather than adding a plausibility clamp in
`internal/datex/afir.go` (`priceComponents`, `pricePerMinute` → `TIME`, ×60). Revisit
a defensive guard if they don't resolve it or if other feeds show the same pattern.
