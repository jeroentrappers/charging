# Data-quality feedback emails

Ready-to-send drafts flagging concrete data-quality issues we hit while
ingesting each source (see the dataset investigation, 2026-07-09). Tone: we're a
consumer of their public/AFIR feed, reporting issues to help — not complaining.

Sender: Jeroen Trappers, Software engineer at Appmire <jeroen@appmire.be> · 0497053310.

**Drafts 1–7 sent 2026-07-09**; **#8 (Fintraffic) sent 2026-08-20** — it lives in
its own file because it needed a longer evidence trail than a table row, and it
is the only one so far to have drawn a substantive reply (see that file).
**#9 (Eco-Movement, on their new Belgian NAP feed) sent 2026-09-04**, with
`ecomovement-divergent-prices-2026-09-04.csv` attached.
Reply / re-verification tracker:

| # | Source | Sent | Reply | Fixed? (re-verified) |
|---|--------|------|-------|----------------------|
| 1 | EnergyVision | 2026-07-09 | — | ❌ status feed still has `pricePerMinute` 19.80 (checked 2026-07-09 PM) |
| 2 | NDW / DOT-NL | 2026-07-09 | 2026-07-10 (pass-through; asked for the location list to forward to the supplier) | list sent 2026-07-10 (`ndw-defects-2026-07-10.csv`); workaround already shipped our side |
| 3 | Eco-Movement | 2026-07-09 | — | ⚠️ **power fixed 2026-09-04** — their new BE NAP feed (`nap-be.eco-movement.com`) carries power on all but 2 of 69,408 connectors (was ~41% missing). The **mislocated points remain** (12 sites / 188 points outside Belgium, 6 of them in the US) — re-reported with the ids in #9 |
| 4 | eRoundup | 2026-07-09 | — | — |
| 5 | EnBW | 2026-07-09 | — | — |
| 6 | EW Pricing GmbH | 2026-07-09 | — | — |
| 7 | Monta | 2026-07-09 | — | — |
| 8 | **Fintraffic (FI)** — draft in [`fintraffic-mail.md`](./fintraffic-mail.md) | 2026-08-20 | **2026-09-02** (Mika Ahvenainen, Development Manager) | ⚠️ partly: the missing records are the **operators'** to submit (Traficom is chasing them) — no Fintraffic-side fix; the unused-tariff over-publication **will** be fixed (`/tariffs` to return only referenced tariffs by default) |
| 9 | **Eco-Movement** (new BE NAP feed) — text below | **2026-09-04** | — | — (awaiting reply; re-check the TotalEnergies price gap, the `taxRate` unit split, the €0 flat fees and the 57 duplicated EVSE ids against a later snapshot) |

Only sources with findings are listed. **No issues found** (nothing to send):
tesla, road, indigo, mob-chargecloudgmbh (minor), and the smaller Mobilithek
feeds. **Government open-data registries** (irve / data.gouv.fr, bnetza) have
issues too (zero-power rows, a few off-country points) but are unattended
datasets — track those as portal feedback rather than email.

For the Mobilithek-brokered German CPOs, the operator contact is usually in the
feed's `/metadata` contact field; recipients below are marked `[contact]` where
we don't yet have a direct address.

---

## 1. EnergyVision — myevplatform@energyvision.be

**Subject:** DATEX II feed — time-price unit looks off (quick heads-up)

Hello EnergyVision team,

Thanks again for the recent feed improvements — the coordinates, identifiers and
gzip are all working well on our side now.

One more thing we noticed while consuming the ad-hoc prices: the
`pricePerMinute` values look too high by what seems like a unit factor. For
example, the station at **Blankenbergse Steenweg 10** reports
`pricePerMinute = 19.80`, and we see 1.04 and 3.60 elsewhere. Read as €/minute
(the DATEX II definition) that's €62–€1188 per hour, which surfaces in our app
as a ~€500 session. We suspect these may actually be **hourly** fees, or carry a
different unit than per-minute.

Could you confirm the intended unit and values for the time component? Happy to
share the exact station list if useful.

Separately, a minor operational note: our scheduled fetches of the status feed
intermittently get an HTTP/2 `GOAWAY` from your server (a few times a day), which
makes some polls fail and retry. Not urgent, but flagging in case it points to a
server-side connection limit.

Thanks very much,
Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 2. NDW / DOT-NL open data — [contact: opendata@ndw.nu] (data originates from Eneco)

**Subject:** charging_point_locations_ocpi — max_electric_power off by 1000× for some AC posts

Hello NDW team,

We use your open OCPI dataset
(`https://opendata.ndw.nu/charging_point_locations_ocpi.json.gz`) and spotted a
unit issue on a subset of connectors — the ones from **Eneco eMobility** in
particular.

For these, `max_electric_power` appears to be **voltage × amperage × 1000**
instead of watts. Example — EVSE `NL-ENE-EEVB-P1402500-1`:

```
power_type: AC_3_PHASE, max_voltage: 230, max_amperage: 33,
max_electric_power: 7590000   → reads as 7590 kW
```

The real figure is 230 V × 33 A × 3 ≈ **22.8 kW** (i.e. `max_electric_power`
should be ~22770). A handful of connectors are affected (we counted ~17 clearly
impossible values, up to 7590 kW). We've worked around it by falling back to
voltage×amperage, but a source-side fix would help everyone consuming the feed.

A couple of smaller things in the same file: ~15 connectors sit at coordinates
`0,0` (null island), and a few dozen fall outside NL (latitude up to 90).

Thank you for publishing this data — it's genuinely useful.

Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

### Reply (2026-07-10) — Marco Dijkstra, NDW

NDW is a pass-through and asked for the affected locations to forward to the
supplying party. Compiled the full list from the live open feed (our DB stores
the corrected values, so the faulty raw values only exist in the source):
**`docs/ndw-defects-2026-07-10.csv`** — 69 defects:

- **13 `power_impossible`** — all Eneco (`NL*ENE*…`): `max_electric_power` is
  ~1000× too high (2222–7590 kW on 230 V AC posts; the unit bug we reported).
- **5 `power_too_high_for_AC`** — all Eneco: 35 / 111 / 350 kW on 230 V×32 A AC
  (physically impossible for AC).
- **13 `coordinates_null_island`** — at `0,0` (ENV, SGM, CCH).
- **38 `coordinates_outside_NL`** — US, India, Dublin, lat 90, a swapped lat/lon
  (`4.28,52.09`), etc. (EFL, EVT, ENE, SGM, SMP, …).

DC "power vs V×A" mismatches were deliberately excluded — for DC
`max_electric_power` is authoritative and `max_amperage` is often just a nominal
cable rating, so those aren't defects.

---

## 3. Eco-Movement (ecomovement feed) — [contact]

**Subject:** BE OCPI feed — missing power ratings and a few mislocated points

Hello Eco-Movement team,

We consume your Belgian charging feed and wanted to share two data-quality
observations, in case they're easy fixes upstream:

- **Power rating missing on ~41% of connectors** (~20,000 of ~49,000) — they
  come through with a power of 0 / unset, so we can't classify them as AC vs
  fast-charge or price a time-based session.
- **~26 connectors are located in the United States** (latitude 39–42, longitude
  −77 to −93) rather than Belgium — likely swapped or placeholder coordinates.

Happy to send the exact EVSE ids for both. Thanks for the feed!

Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 4. eRoundup (mob-eround, via Mobilithek) — [contact]

**Subject:** AFIR feed — ~600 charge points with out-of-range coordinates

Hello,

We ingest your charging data via the German Mobilithek and noticed roughly
**600 charge points with implausible coordinates** — longitudes up to 132° and
down to −55°, latitudes down to −27°, i.e. well outside Germany. It looks like a
subset may have swapped latitude/longitude or carry placeholder values.

Could you take a look? We're glad to provide the affected identifiers.

Thanks,
Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 5. EnBW (mob-enbwag, via Mobilithek) — [contact]

**Subject:** AFIR feed — uniform energy price + time fee on all stations

Hello EnBW team,

While consuming your AFIR feed we noticed that **all ~12,000 stations carry the
same two price components**: an energy price of exactly `0.66386555 €/kWh` and a
time component of `12 €/h`. Two things we wanted to check:

- The energy value's long decimal (`0.66386555`) looks like a converted or
  default figure rather than a tariff-sheet price — is that intended?
- The flat `12 €/h` time component, applied everywhere, dominates our
  comparable-session estimate (pushing many stations above €100). If that's a
  blocking/idle fee rather than an active-charging rate, tagging it as
  `PARKING_TIME` would let consumers exclude it correctly.

Happy to share specifics. Thanks for making the data available.

Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 6. EW Pricing GmbH (mob-ewpricinggmbh, via Mobilithek) — [contact]

**Subject:** AFIR feed — €360/h time price and duplicate energy components

Hello,

We consume your charging tariffs via the German Mobilithek and hit two issues on
the same stations (e.g. OCPI id `9660f125-3463-5506-8eb3-57dc969e4cc5`):

- a **time component of `360 €/h`**, which is almost certainly a unit or data
  error (it produces an ~€8,500 session in our comparison);
- **multiple conflicting ENERGY components in one tariff element** (0.45, 0.50,
  0.51, 0.55, 0.56, 0.59, 0.64 €/kWh) with no restrictions to disambiguate them,
  so a consumer can't tell which price applies.

Could you check these? Glad to provide the full list.

Thanks,
Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 7. Monta (monta feed) — [contact]

**Subject:** BE feed — a couple of outlier values

Hello Monta team,

Two small data points we spotted in your Belgian feed:

- one station with an **energy price of €7.50/kWh** (roughly 15× the ~€0.30–0.50
  norm) — likely a typo or a per-session value in the per-kWh field;
- one station located at **latitude 72° (Arctic)**, longitude 0.23 — probably a
  coordinate error.

Nothing urgent — just flagging so they can be corrected at source. Thanks!

Jeroen Trappers — Appmire — jeroen@appmire.be — 0497053310

---

## 9. Eco-Movement — support@eco-movement.com (new Belgian NAP feed) — SENT 2026-09-04

**Subject:** BE NAP DATEX II feed — thanks, plus six observations from ingesting it

Hello Peter / Eco-Movement team,

Thank you for the token for the new Belgian NAP publication
(`nap-be.eco-movement.com/datex2/v1/`). We switched over to it this week and it
is a big step up on the old XML export: 16,700 sites / 69,408 connectors, live
status on every refill point, an ad-hoc price on 81% of them, and — the thing we
reported back in July — a power rating on all but two connectors, where the old
feed was missing about 40%. We're also glad to see `timeBasedApplicability` on
the idle fees; we now price those correctly because of it.

Six things we noticed while ingesting, in case they're easy to act on:

**1. TotalEnergies publishes almost no prices.** 12,585 of their 12,778 refill
points carry no `energyRateUpdate` at all — and they account for 99% of every
unpriced point in the feed (12,585 of 12,689). The 193 points they do price come
through fine (€0.79/kWh DC). They're the largest network in the publication by
point count, so this one operator is the whole gap between 81% and ~99% price
coverage. Is that something you can chase with them, or is it a matter for the
Belgian NAP / the operator directly?

**2. `taxRate` is published in two different units.** Most prices state
`"taxRate": 21` for Belgian VAT, but 1,314 of them (all TotalEnergies) state
`"taxRate": 0.21` for what we believe is the same 21%. We read a value above 1
as a percentage and a value at or below 1 as a fraction, which gives
TotalEnergies' net 0.6529 → €0.79/kWh — a plausible round consumer price, so we
think that reading is right. Could you confirm which form is intended, and
ideally normalise it? As published, a consumer that takes the number literally
either overcharges by 21% or undercharges by the same.

**3. A `flatRate` of 0.00 as the only price on ~693 points.** 544 of them are
Eneco (whose other 2,324 points carry normal per-kWh prices), the rest spread
over NUMOBI, Stroohm, G&V, CenEnergy and a few others — 19 distinct
`energyRate` ids in total. Taken literally, that says a session costs €0, so
those points sorted to the top of our "cheapest charger nearby" list ahead of
genuinely priced ones. We now treat a rate that prices nothing as "no tariff
published" instead. If these operators really do charge nothing, the AFIR
`priceType` `free` (or a 0.00 `pricePerKWh`) would say so unambiguously; if the
tariff simply isn't filed, omitting the `energyRateUpdate` — as you already do
for TotalEnergies — is clearer than a zero fee.

**4. Twelve sites are still outside Belgium** (188 refill points) — the one
finding from our July note that carried over. Six are Eneco sites plotted in the
United States, and the rest look like placeholders or swapped coordinates:

| site `idG` | latitude, longitude | points |
|---|---|---|
| `Eneco-FEF42BB2-11B1-49AB-B2D5-C4AC6BB590E9` | 42.01916, −93.44568 (Iowa) | 20 |
| `Eneco-6AD7C846-FFDB-4522-B400-CA6FCA55B2FA` | 32.52707, −92.29220 (Louisiana) | 8 |
| `Eneco-DFE90232-D5DB-4C53-A35D-789CFB1AA64C` | 39.64574, −77.73575 (Maryland) | 6 |
| `Eneco-DC0F4263-4BF9-45BB-930A-EBF7D68DD3BF` | 37.55969, −86.76846 (Kentucky) | 2 |
| `Eneco-C8F379A7-8926-4612-836F-338F59F639BB` | 40.54271, −78.78938 (Pennsylvania) | 2 |
| `Eneco-B41F4B1A-DFA8-49C6-9576-4ADD015C9C97` | 38.13606, −89.10456 (Illinois) | 1 |
| `Stroohm-89332768` | −43, −43 (South Atlantic) | 24 |
| `Stroohm-90919929` | 48.876667, 123.393333 (China) | 5 |
| `Pluginvest-90355655` | 48.876667, 123.393333 (same pair) | 2 |
| `50five-90248112` | 52.082855, 4.301847 (The Hague, NL) | 112 |
| `Sparki-7dd0ba37-19ac-4b7c-94a3-bdd6dfceda7b` | 50.107491, 19.832203 (Poland) | 4 |
| `ChargePoint-BECPIL6986155` | 44.908544, 1.471283 (France) | 2 |

All twelve carry `countryCode: BE` in their address, so a bounding-box check
against the stated country would catch them at your end.

**5. Two prices for identical plugs at the same site**, and one publishing
artefact behind some of it.

At **83 sites**, two refill points of the same current type, power and connector
type quote different `pricePerKWh` — median 1.35×, up to **2.76×** (MobilityPlus,
Leuvensesteenweg 186 Diest: €0.3327 on two 7.4 kW AC points, €0.9184 on a third).
Each side references a different `energyRate` id, and 16 of the price pairs recur
at more than one site, so we read this as deliberate pricing (host-set tariffs?)
rather than an error — but we'd rather ask than guess, because a driver at those
sites pays up to triple depending on which pole they pick. The full list is
attached (`ecomovement-divergent-prices-2026-09-04.csv`: site, address,
coordinates, both prices, point counts, rate ids, EVSE ids).

Related, and this one does look like an artefact: **57 EVSE ids are published
twice**, under two different refill-point `idG`s. In 21 of those pairs the two
copies carry different prices, and the pattern is consistent — the `inoperative`
copy carries the operator's OLD tariff while the `available` copy carries the
current one. All 19 Lidl cases look like this (e.g. `BE*LDL*E00000011`:
inoperative at €0.363/kWh beside available at €0.7016; the cheap figures are
€0.20/€0.30 net, which we believe were Lidl's prices some years ago), plus 2 at
Q8 electric. Since both copies share one roaming EVSE id, a consumer keying on
that id has to guess which record is current — and the two endpoints answer
differently: `GET /datex2/v1/status/BE*LDL*E00000011` returns only the
**inoperative** copy at €0.363, while the same id on `/datex2/v1/locations`
carries both, the live one priced €0.7016. So a consumer using your per-EVSE
status endpoint gets the retired record's price and availability. Dropping the
decommissioned copies would settle both.

(For what it is worth, the timestamps do let a consumer tell them apart: the
retired copies' `lastUpdated` has not moved since 2026-09-03T10:08:40Z, while
their live twins are touched through the day. That is what we now use to pick
the right one, but we would rather not have to.)

**6. Duplicate `energyPrice` entries.** Many rates repeat the same price under
different `priceGroupIndex` values — e.g. the same `pricePerMinute` twice at
index 1 and twice at index 2. Harmless (we collapse identical entries), but it
roughly doubles the size of those payloads.

And two questions about consuming the feed:

- **Polling cadence.** Since there is no bulk status endpoint, the only way to
  refresh availability is to walk all 17 pages, which is ~104 MB per pass. We
  currently do that hourly (plus a daily pass for prices) to be polite. What
  cadence are you comfortable with — and is a status-only or delta endpoint on
  the roadmap? That would let us follow your 60-second refresh instead of
  sampling it hourly.
- **Compression.** The endpoint doesn't honour `Accept-Encoding: gzip`; the JSON
  compresses about 10:1, so enabling it would cut both our bandwidth and yours
  by an order of magnitude.

Minor, and only if you're collecting them: 128 connectors report more than
400 kW, 14 AC connectors report over 44 kW, and 2 report 0 kW.

Happy to send the exact EVSE ids behind any of these. Thanks again for the new
feed and for turning the token around so quickly.

Best regards,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310
