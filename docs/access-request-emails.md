# API-key request emails

Ready-to-send drafts to obtain OCPI access for the sources in
[`sources.md`](./sources.md). Access is a free, non-discriminatory right under
**AFIR (Reg. EU 2023/1804) Article 20**, so these are requests, not negotiations.

Sender: Jeroen Trappers, Software engineer at Appmire <jeroen@appmire.be>.

Track replies in the checklist at the bottom of `sources.md`.

---

## 1. EnergyVision — myevplatform@energyvision.be

**Subject:** OCPI API key request — public charging data (AFIR Art. 20)

Hello EnergyVision team,

I found your "EnergyVision Public Charging Network" dataset on Belgium's National
Access Point (transportdata.be) and would like to request access to your OCPI
feed.

We are building an app, a consumer service that compares the price and
availability of public EV chargers in Belgium. For this we'd like to
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
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 2. Tesla Belgium — spolireddi@tesla.com (cc: aboumssimrat@tesla.com)

**Subject:** OCPI 2.2.1 access request — Belgian public charging data (AFIR Art. 20)

Hello,

Via Belgium's National Access Point (transportdata.be) I found your "Tesla-API"
charging dataset, published under AFIR Article 20 at
`https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/`.

We're building an app, a service comparing public EV-charging price and
availability in Belgium, and would like to consume your OCPI 2.2.1
**Locations** (with live status) and **Tariffs** modules.

Could you please share the access token/credentials and confirm the available
modules and any polling guidance? As this falls under AFIR Article 20, access
should be free and non-discriminatory.

Many thanks,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 3. Monta — data@monta.com

**Subject:** OCPI / AFIR charge-point data access — Belgium (Art. 20)

Hello Monta team,

I found your public charging infrastructure dataset on Belgium's National Access
Point (transportdata.be), referencing your AFIR charge-points API.

We're building an app, comparing public EV-charging price and availability in
Belgium. Your open **Public API** charge-points list
(`/api/v1/afir/charge-points?country=BE`) already gives us locations — but the
**ad-hoc price and live status are on the per-EVSE status endpoint**, which needs
credentials. Per AFIR Article 20 this data is available free of charge and
without discrimination.

Could you grant us access to the Public API (credentials for the AFIR
`…/status` endpoint), and confirm rate limits? We'd use it to show the live
ad-hoc price for a charger a user selects.

Thanks in advance,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 4. Road — roaming-dev@road.io

**Subject:** OCPI feed access — Belgian public charging (AFIR Art. 20)

Hello Road team,

Through Belgium's National Access Point (transportdata.be) I found your
"Road Public Charging Network" dataset. The published resource is a static
`locations.json` file; we'd like to consume your live OCPI feed if available.

We're building an app, comparing public EV-charging price and availability in
Belgium. Specifically we need the **OCPI Locations (with EVSE status)
and Tariffs** modules.

Could you provide the OCPI base/versions URL, the version, an access token, and
note whether ad-hoc tariffs and real-time status are included? Per AFIR Article
20 this should be free and non-discriminatory.

Kind regards,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 5. Eco-Movement — nap@eco-movement.com

**Subject:** Access to Belgian charging data — price & availability (AFIR Art. 20)

Hello Peter / Eco-Movement team,

I found your "Public charging infrastructure — selected CPOs" dataset on Belgium's
National Access Point (transportdata.be), covering ~20 networks. This breadth is
exactly what we need.

We're building an app, a service comparing public EV-charging price and
availability in Belgium. Two questions:

1. Does the AFIR NAP feed (the DATEX II locations endpoint) include **ad-hoc
   price and dynamic availability**, or only static location/AFIR data?
2. If price/availability aren't in the NAP feed, is access to your **OCPI Data
   API** (which does carry tariffs and status) available to us — and under what
   terms (the AFIR Article 20 free-access basis, or a commercial agreement)?

We can consume either DATEX II or OCPI. Any documentation, an access token, and a
note on terms would be much appreciated.

Best regards,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 6. Austria E-Control — via https://admin.ladestellen.at/#/api/registrieren

Austria's national charging register is the one remaining source that publishes
**live status AND ad-hoc price** but needs a registration (per its NAP metadata
on mobilitydata.gv.at; `api.e-control.at/charge/1.0/…` answers 401 without a
key). Self-service registration first — use this only if the form stalls or the
scope is unclear. German, since the register is German-language.

**Subject:** API-Zugang zum Ladestellenverzeichnis (DATEX II) — AFIR Art. 20

Sehr geehrtes E-Control-Team,

wir betreiben einen Verbraucherdienst, der Preis und Verfügbarkeit öffentlicher
Ladepunkte in Europa vergleicht (charging.appmire.be), und möchten das
**nationale Ladepunkteregister** über Ihre API einbinden. Laut dem Datensatz auf
mobilitydata.gv.at wird es als **DATEX II v3.2** bereitgestellt und enthält
sowohl den **Status (frei/besetzt)** als auch den **Ad-hoc-Preis**.

Dürften wir Sie um Folgendes bitten:

- einen API-Zugang bzw. die Freigabe unserer Registrierung über
  admin.ladestellen.at;
- die Endpunkte der statischen Tabelle und der Statuspublikation
  (EnergyInfrastructureTablePublication / …StatusPublication);
- Hinweise zu Abrufintervallen, die wir einhalten sollen.

Wir lesen DATEX II v3 bereits für mehrere nationale Zugangspunkte (BE, ES, PT)
und können das Format unverändert verarbeiten. Gemäß AFIR Artikel 20 ist der
Zugang unentgeltlich und nichtdiskriminierend.

Vielen Dank und freundliche Grüße,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310

---

## 7. Poland UDT (EIPA) — eipa@udt.gov.pl

EIPA's `dynamic.json` carries **status + tariffs in PLN**; access needs a
registration and comes with an hourly download limit.

**Subject:** EIPA open data access — charging point status and prices (AFIR Art. 20)

Dear UDT / EIPA team,

We run a consumer service comparing the price and availability of public EV
chargers across Europe (charging.appmire.be), built entirely on open national
access point data. We would like to include Poland via the **EIPA** open data
files documented at `https://eipa.udt.gov.pl/reader/docs` — in particular
`station.json`, `point.json` and `dynamic.json` (status and current tariffs).

Could you please:

- register us for file access and confirm how to authenticate;
- confirm the hourly download limit you would like us to stay within, and how
  often `dynamic.json` is regenerated;
- confirm the licence/attribution you require.

If a DATEX II endpoint is or becomes available alongside the JSON files (we saw
the `EVChargingInfra_v3.2` schema published on the site), we can consume that
format directly as well.

Thank you very much,
Jeroen Trappers
Software engineer at Appmire — jeroen@appmire.be — 0497053310
