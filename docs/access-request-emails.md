# API-key request emails

Ready-to-send drafts to obtain OCPI access for the sources in
[`sources.md`](./sources.md). Access is a free, non-discriminatory right under
**AFIR (Reg. EU 2023/1804) Article 20**, so these are requests, not negotiations.

**Before sending, fill the placeholders:** `[role]`, `[company]`, `[phone]`,
`[app name]`, `[app URL]`. Sender assumed: Jeroen Trappers
<jeroen.trappers@secutec.com>.

Track replies in the checklist at the bottom of `sources.md`.

---

## 1. EnergyVision — myevplatform@energyvision.be

**Subject:** OCPI API key request — public charging data (AFIR Art. 20)

Hello EnergyVision team,

I found your "EnergyVision Public Charging Network" dataset on Belgium's National
Access Point (transportdata.be) and would like to request access to your OCPI
feed.

We are building [app name], a consumer service that compares the price and
availability of public EV chargers in Belgium ([app URL]). For this we'd like to
consume your OCPI 2.1.1 interface at `https://ocpi.energyvision.be/cpo/2.1.1/` —
specifically the **Locations** (including live EVSE status) and **Tariffs**
modules, so we can show accurate ad-hoc prices and availability.

Per AFIR Article 20 this data is available free of charge and without
discrimination. Could you please provide:

- the API token (credentials) for the feed;
- confirmation of the modules exposed (Locations, Tariffs) and the OCPI version;
- any polling/rate-limit guidance you'd like us to respect.

Thank you very much,
Jeroen Trappers
[role], [company] — jeroen.trappers@secutec.com — [phone]

---

## 2. Tesla Belgium — spolireddi@tesla.com (cc: aboumssimrat@tesla.com)

**Subject:** OCPI 2.2.1 access request — Belgian public charging data (AFIR Art. 20)

Hello,

Via Belgium's National Access Point (transportdata.be) I found your "Tesla-API"
charging dataset, published under AFIR Article 20 at
`https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/`.

We're building [app name], a service comparing public EV-charging price and
availability in Belgium ([app URL]), and would like to consume your OCPI 2.2.1
**Locations** (with live status) and **Tariffs** modules.

Could you please share the access token/credentials and confirm the available
modules and any polling guidance? As this falls under AFIR Article 20, access
should be free and non-discriminatory.

Many thanks,
Jeroen Trappers
[role], [company] — jeroen.trappers@secutec.com — [phone]

---

## 3. Monta — data@monta.com

**Subject:** OCPI / AFIR charge-point data access — Belgium (Art. 20)

Hello Monta team,

I found your public charging infrastructure dataset on Belgium's National Access
Point (transportdata.be), referencing your AFIR charge-points API.

We're building [app name], comparing public EV-charging price and availability in
Belgium ([app URL]). We'd like programmatic access to your Belgian public
charging data — ideally the **OCPI Locations (with live status) and Tariffs**
modules so we can include ad-hoc pricing and real-time availability.

Could you let us know the endpoint base URL, OCPI version, how to authenticate
(token), and any usage terms? Per AFIR Article 20 this data should be available
free of charge and without discrimination.

Thanks in advance,
Jeroen Trappers
[role], [company] — jeroen.trappers@secutec.com — [phone]

---

## 4. Road — roaming-dev@road.io

**Subject:** OCPI feed access — Belgian public charging (AFIR Art. 20)

Hello Road team,

Through Belgium's National Access Point (transportdata.be) I found your
"Road Public Charging Network" dataset. The published resource is a static
`locations.json` file; we'd like to consume your live OCPI feed if available.

We're building [app name], comparing public EV-charging price and availability in
Belgium ([app URL]). Specifically we need the **OCPI Locations (with EVSE status)
and Tariffs** modules.

Could you provide the OCPI base/versions URL, the version, an access token, and
note whether ad-hoc tariffs and real-time status are included? Per AFIR Article
20 this should be free and non-discriminatory.

Kind regards,
Jeroen Trappers
[role], [company] — jeroen.trappers@secutec.com — [phone]

---

## 5. Eco-Movement — nap@eco-movement.com

**Subject:** Access to Belgian charging data — price & availability (AFIR Art. 20)

Hello Peter / Eco-Movement team,

I found your "Public charging infrastructure — selected CPOs" dataset on Belgium's
National Access Point (transportdata.be), covering ~20 networks. This breadth is
exactly what we need.

We're building [app name], a service comparing public EV-charging price and
availability in Belgium ([app URL]). Two questions:

1. Does the AFIR NAP feed (the DATEX II locations endpoint) include **ad-hoc
   price and dynamic availability**, or only static location/AFIR data?
2. If price/availability aren't in the NAP feed, is access to your **OCPI Data
   API** (which does carry tariffs and status) available to us — and under what
   terms (the AFIR Article 20 free-access basis, or a commercial agreement)?

We can consume either DATEX II or OCPI. Any documentation, an access token, and a
note on terms would be much appreciated.

Best regards,
Jeroen Trappers
[role], [company] — jeroen.trappers@secutec.com — [phone]
