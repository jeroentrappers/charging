# Data-quality feedback emails

Ready-to-send drafts flagging concrete data-quality issues we hit while
ingesting each source (see the dataset investigation, 2026-07-09). Tone: we're a
consumer of their public/AFIR feed, reporting issues to help — not complaining.

Sender: Jeroen Trappers, Software engineer at Appmire <jeroen@appmire.be> · 0497053310.

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
