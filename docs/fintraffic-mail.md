# Fintraffic (FI) — data-quality report: AFIR tariff records

Draft message about the gap found in Finland's AFIR charging feed on 2026-08-20:
the locations publication references tariff ids that no tariff publication
contains, which leaves 47% of Finnish connectors unpriceable. Numbers below were
measured against the live feed; see also the `fi-fintraffic` row in
[`sources.md`](./sources.md).

## Where to send it

**To:** `info@fintraffic.fi` — Fintraffic's official address for general
enquiries (switchboard +358 29 450 7000; postal: P.O. Box 71, 00241 Helsinki).
It is a general inbox rather than a team address, so the message opens by asking
them to pass it to whoever maintains the AFIR charging APIs.

Useful alternatives, either instead of or after the email:

- **Road-traffic Google Group** — `roaddigitrafficfi@googlegroups.com`
  (web: <https://groups.google.com/g/roaddigitrafficfi>). Digitraffic's support
  page says: "Open developer communities form the support channel for the
  Digitraffic service… Fintraffic specialists participate." It reaches the
  maintainers directly and the thread helps other consumers hitting the same gap.
  NOTE: this list is **public** — strip the phone number from the signature
  before posting there.
- **Palauteväylä feedback channel** — <https://palautevayla.fi/aspa?lang=en>,
  the route linked from both AFIR pages, if a tracked ticket is preferred.
- **GitHub `tmfg/digitraffic`** — documentation fixes only, not data content.

**Sent 2026-08-20 to `info@fintraffic.fi`; answered 2026-09-02** — see
[the reply](#reply-2026-09-02--mika-ahvenainen-fintraffic) below. For any
follow-up, use the team address they gave us: **`digitraffic@fintraffic.fi`**
(or `mika.ahvenainen@fintraffic.fi` to continue this thread directly).

Write in **English** — the AFIR API documentation and the Google Group are in
English.

---

## Subject

`AFIR charging API: tariff records missing for some parties (425 tariff ids referenced but not published)`

## Message

Hello Fintraffic team,

Could I ask you to forward this to whoever looks after the AFIR charging-point
APIs on Digitraffic? It is a data-quality observation rather than a support
request, and I could not find a direct address for that team.

First of all, thank you for the AFIR charging APIs on `afir.digitraffic.fi`.
We consume a number of national access points across Europe, and yours is the
most pleasant of them to work with: one national service instead of per-CPO
onboarding, OCPI-shaped JSON *and* DATEX II side by side, snapshots regenerated
every minute, an MQTT stream, and no registration needed. The fact that you
convert the operators' OCPI to DATEX II for them, free of charge, is genuinely
appreciated by those of us on the consuming end.

For context: we run a small consumer service that compares the ad-hoc (drive-up)
price and availability of public charging points, built entirely on open national
access point data — Finland alongside BE, NL, DE, FR, ES, PT, AT, PL and CH. We
started ingesting your feed a couple of days ago (~3,800 locations / ~20,000
connectors) and while validating the prices we ran into something we think is on
your side rather than ours, so we wanted to report it carefully.

**The observation.** The locations publication references tariff ids that no
tariff publication contains. Measured on 2026-08-20:

| | connectors | share |
|---|---|---|
| Tariff reference resolves against `/tariffs` | 3,465 | 17% |
| **Tariff reference present but the tariff record does not exist** | **9,460** | **47%** |
| No `tariffIds` on the connector at all | 7,085 | 35% |
| Total | 20,010 | |

There are **425 distinct tariff ids** referenced by those 9,460 connectors that
we cannot resolve. They are concentrated in a few parties:

| Party | Connectors | With a tariff reference | Resolving | Tariff records published |
|---|---|---|---|---|
| `FI*001` Liikennevirta | 6,948 | 6,943 | 0 | **0** |
| `FI*EPA` eParking | 1,593 | 1,593 | 0 | **0** |
| Tesla | 446 | 446 | 0 | 0 |
| Porsche Charging Service | 4 | 4 | 0 | 0 |

For the two largest, `/tariffs` contains **no records at all** for the party,
even though several thousand of their connectors reference tariff ids — which is
why we suspect the Tariffs module simply isn't being picked up for them, rather
than individual rows going missing.

**What we checked before writing.** We wanted to be sure this wasn't our own
parsing:

- The 425 ids match nothing in `/tariffs` under any normalisation we tried —
  exact, case-insensitive, punctuation-stripped, or reassembled as
  `countryCode + partyId + id`. None of them is even a substring of a published
  id.
- The id *shape* differs between the two sides: 420 of the 425 unresolved ids are
  UUIDs, whereas `/tariffs` publishes 2,267 hex-32 ids, 536 asterisked ids
  (`FI*XXX*…`) and only 3 UUIDs. Example unresolved ids:
  `a125f1ce-5f14-4ee4-bdc5-0236c5b5a241` (eParking) and
  `FI001TWVZFXLFU2YJMVQHNK2IAJ6P9JF5QK9` (Liikennevirta).
- We also checked the DATEX II encoding in case it was more complete: the beta
  `/api/charging-network/beta/tariffs/datex2-3.7` publication carries exactly
  2,969 `energyRate` entries — the same count as the JSON `/tariffs` — and none
  of the 425 ids appear there either.
- We paginate with `limit=ALL` and the response reports `pagination.limit: 2969`
  with no `nextCursor`, so we don't believe we're missing a page.

**The ask.** Could the tariff records for these parties (especially `FI*001` and
`FI*EPA`) be included in the tariff publication? As far as we can tell, adding
those 425 records would let roughly 9,460 connectors be priced, taking the share
of Finnish connectors with a usable ad-hoc price from about 17% to about 64% — a
large improvement for a small number of records.

**Two things that are *not* problems, to save you time:**

- The remaining 35% of connectors carry no `tariffIds` at all (ABC, K-lataus,
  Plugit Finland, Aimo Charge, Lidl and others). We understand that is a matter
  for those operators to submit under AFIR Article 20, and not something you can
  publish on their behalf.
- About 77% of the published tariffs are referenced by no Finnish connector — for
  example Allego publishes 2,029 tariffs against 12 Finnish connectors, all of
  which resolve correctly. That looks like operators pushing their full European
  catalogue into the Finnish feed, which is harmless. We mention it only so it
  isn't mistaken for the same issue.

We are of course happy to be told we have misread the feed. If it helps, we can
send the full list of 425 unresolved ids, or re-run our check against a test
environment after any change — just say what would be most useful.

Thank you again for making this data available so openly, and for the care that
has clearly gone into the service.

Kind regards,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310
(omit the phone number if this is posted to the public Google Group instead)

---

### Reply (2026-09-02) — Mika Ahvenainen, Fintraffic

Development Manager, Data and Information Services. Answered both findings,
splitting them the same way the report did.

**1. The missing tariff records — not Fintraffic's to fix.** Digitraffic
republishes what the operators deliver, so the absent records for `FI*001`
Liikennevirta and `FI*EPA` eParking have to be corrected in those operators'
own data delivery. He noted that **Traficom** (the Finnish Transport and
Communications Agency, acting as the AFIR National Body) is actively
communicating with the operators and reminding them of their obligations, and
expects tariff coverage to increase in the near future.

So the 47% unpriceable share stands for now and there is no fix to wait for on
the API side — it will improve operator by operator instead. Nothing to do but
re-measure; see below.

**2. The unused tariffs — being fixed.** He took this to their development team,
and they decided to **change the default behaviour of `/tariffs` to return only
tariffs that are referenced from the connectors**, adding a parameter to query
all tariffs in their database. No date was given.

Harmless for us: `Client.tariffs` fetches `/tariffs` with `limit=ALL` and joins
by id, so a smaller referenced-only response is strictly less work and nothing
in the adapter or in `/api/status` keys off the tariff count. Worth knowing at
cutover only because the published tariff total will drop by roughly 77%
(2,969 → ~700), which is the change landing and not a feed regression. We do not
need the new all-tariffs parameter — we only ever resolve referenced ids.

He also thanked us for sending the `Digitraffic-User` header
(`fintraffic.UserAgent`, set in `Client.get`), which their fair-use policy asks for. Keep it.

## After sending

- [x] Ticked off in [`data-quality-emails.md`](./data-quality-emails.md) with the
  reply summarised above.
- As operators start submitting, the `fi-fintraffic` price coverage in
  `/api/status` should climb from ~17% towards ~64% (the source runs a daily
  price pass); the numbers in [`sources.md`](./sources.md) need updating then.
  Expect this in steps as each operator fixes its delivery, not in one jump.
- Re-measure every month or two rather than watching it: worth a check around
  **2026-11**, and again if `/tariffs` suddenly shrinks (that is the
  referenced-only default landing, see the reply).
- Worth re-measuring before chasing: the gap was stable at 3,460 → 3,465
  resolving connectors across 2026-08-19/20, so it is structural rather than a
  transient.
