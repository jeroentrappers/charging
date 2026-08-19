// Package source resolves CPO records from the registry into ready-to-use OCPI
// clients, wiring in auth tokens from the environment, and seeds the known
// Belgian NAP sources.
package source

import (
	"os"

	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/store"
)

// Source pairs a CPO registry entry with its resolved token.
type Source struct {
	CPO   store.CPO
	Token string
}

// Client builds an OCPI client for this source, honoring its OCPI version.
func (s Source) Client() *ocpi.Client {
	return ocpi.NewVersioned(s.CPO.OCPIBaseURL, s.Token, s.CPO.OCPIVersion)
}

// HasToken reports whether a usable token was resolved.
func (s Source) HasToken() bool { return s.Token != "" }

// Ready reports whether the source can be polled: it either has a token, or is
// an open feed that declares no token (TokenEnv unset, e.g. Road's public file).
func (s Source) Ready() bool { return s.Token != "" || s.CPO.TokenEnv == "" }

// Resolve turns CPO registry rows into sources. The token is the DB-stored
// value when set (managed via the admin API/CLI), otherwise the environment
// variable named by CPO.TokenEnv.
func Resolve(cpos []store.CPO) []Source {
	out := make([]Source, 0, len(cpos))
	for _, c := range cpos {
		tok := c.Token
		if tok == "" && c.TokenEnv != "" {
			tok = os.Getenv(c.TokenEnv)
		}
		out = append(out, Source{CPO: c, Token: tok})
	}
	return out
}

// Seeds returns the known Belgian NAP charging OCPI sources to register on
// startup (disabled). Enable a source once its token is set and the client
// supports its OCPI version. See docs/sources.md for the full catalogue
// (incl. DATEX II aggregators like Eco-Movement that need a separate reader).
func Seeds() []store.CPO {
	return []store.CPO{
		{
			// 🇧🇪 EnergyVision DATEX II (AFIR v3.7, docs 2026-06-30): a static table
			// (24h cache) plus a 60s status feed with live availability AND
			// station-level ad-hoc price updates — parsed by the shared AFIR reader.
			// ~1,240 sites / ~3,765 charging points with coordinates + street
			// addresses (published on aegi:entrance since their 2026-07-08 fix).
			// Bearer API key from myevplatform@energyvision.be; keys EXPIRE EVERY
			// 6 MONTHS and must be re-requested by email.
			ID:          "energyvision",
			Name:        "EnergyVision",
			OCPIBaseURL: "https://datex.cpo.energyvision.be/datex/energy-infrastructure-table|https://datex.cpo.energyvision.be/datex/energy-infrastructure-status",
			SourceType:  "datex_afir",
			TokenEnv:    "ENERGYVISION_TOKEN",
			Country:     "BE",
			PollCron:    "0 4 * * *", // daily; the table is server-cached for 24h
			StatusCron:  "* * * * *", // matches the feed's 60s regeneration; the sig cache makes unchanged polls cheap
			Enabled:     true,        // polled once ENERGYVISION_TOKEN is set
		},
		{
			// 🇧🇪 Tesla Belgium roaming feed (transportdata.be NAP). OCPI 2.2.1,
			// validated live: ~92 locations with real-time EVSE status
			// (AVAILABLE/CHARGING/OUTOFORDER). LOCATIONS-ONLY — Tesla advertises no
			// tariffs module (returns "module not supported"), so the feed ingests
			// coverage + availability but no price. The NAP credential is handed out
			// pre-base64-encoded: set TESLA_TOKEN to "base64:<token>" so the OCPI
			// client sends it verbatim instead of re-encoding it. Only polled once
			// the token is set (Ready() requires it).
			ID:          "tesla",
			Name:        "Tesla Belgium",
			OCPIBaseURL: "https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/",
			OCPIVersion: "2.2.1",
			TokenEnv:    "TESLA_TOKEN",
			Country:     "BE",
			PollCron:    "0 4 * * *",
			StatusCron:  "*/5 * * * *", // Tesla refreshes every 5 min
			Enabled:     true,
		},
		{
			// Monta Public API: open AFIR list (locations) + authed per-EVSE
			// status (availability + ad-hoc price, Monta-party EVSEs only).
			// Token = "clientId:clientSecret" via env MONTA_CREDS. Per-EVSE +
			// rate-limited, so the bulk location poll is daily and a continuous
			// background crawl cycles per-EVSE status+price under the throttle.
			ID:          "monta",
			Name:        "Monta",
			OCPIBaseURL: "https://public-api.monta.com",
			SourceType:  "monta",
			TokenEnv:    "MONTA_CREDS",
			Country:     "BE",
			PollCron:    "0 3 * * *",
			StatusCron:  "0 3 * * *", // per-EVSE + rate-limited -> daily
			// Enabled, but only actually polled/crawled once MONTA_CREDS is set
			// (Ready() requires the token); without creds the scheduler skips it.
			Enabled: true,
		},
		{
			// Open static OCPI 2.2.1 files (no token) — real data available now.
			// OCPIBaseURL is the directory hosting locations.json + tariffs.json.
			ID:          "road",
			Name:        "Road",
			OCPIBaseURL: "https://roaming.road.io/files/9ef09c78-2666-418a-aa45-4f2261e2e305",
			OCPIVersion: "2.2.1",
			SourceType:  "ocpi_file",
			Country:     "BE",
			PollCron:    "0 5 * * *",    // daily price refresh
			StatusCron:  "*/15 * * * *", // availability every 15 min (5 MB file)
			Enabled:     true,           // open data, no key required
		},
		{
			// 🇳🇱 NL DOT-NL (NDW) — the Dutch AFIR National Access Point. Open,
			// no-key, OCPI 2.2.1 gzipped bulk files: ~88k locations WITH live EVSE
			// status AND structured ad-hoc tariffs (incl. Fastned, Allego, Tesla…).
			// OCPIBaseURL is the locations .json.gz; the feed derives the tariffs URL.
			ID:          "dotnl",
			Name:        "NDW · DOT-NL (NL)",
			OCPIBaseURL: "https://opendata.ndw.nu/charging_point_locations_ocpi.json.gz",
			OCPIVersion: "2.2.1",
			SourceType:  "ocpi_file_gz",
			Country:     "NL",
			PollCron:    "0 4 * * *", // daily price refresh
			StatusCron:  "0 * * * *", // hourly availability — cheap now: the engine diffs the feed and only writes connectors that changed
			Enabled:     true,        // open data, no key required
		},
		{
			// 🇩🇪 DE Bundesnetzagentur Ladesäulenregister — official national
			// registry, ~134k stations, open CSV (no key). LOCATION-ONLY: no price,
			// no live status. OCPIBaseURL is the landing page; the feed scrapes the
			// current dated .csv link. Big file → poll monthly.
			ID:          "bnetza",
			Name:        "Bundesnetzagentur (DE)",
			OCPIBaseURL: "https://www.bundesnetzagentur.de/DE/Fachthemen/ElektrizitaetundGas/E-Mobilitaet/Ladesaeulenkarte/start.html",
			SourceType:  "bnetza",
			Country:     "DE",
			PollCron:    "0 5 2 * *", // monthly (2nd, 05:00) — registry refreshes ~monthly
			StatusCron:  "0 5 2 * *", // no live status; keep it monthly too
			Enabled:     true,
		},
		{
			// 🇫🇷 FR consolidated IRVE (transport.data.gouv.fr) — national dataset,
			// ~230k points, open GeoJSON (Licence Ouverte), plus the consolidated
			// national DYNAMIC file (CSV, ~8 MB) joined on id_pdc_itinerance, which
			// is the only availability France publishes. Still NO price: the static
			// schema carries only a free-text tariff we ignore.
			// The 585 MB static is streamed and cached in-process between passes,
			// so the daily availability pass costs the small dynamic file.
			// The dynamic URL is the proxy's NAMED slug: the dataset's
			// /resources/<numeric-id>/download form just 302s here, and the number
			// changes when the publisher re-uploads.
			ID:          "irve",
			Name:        "transport.data.gouv.fr (FR)",
			OCPIBaseURL: "https://www.data.gouv.fr/api/1/datasets/r/7eee8f09-5d1b-4f48-a304-5e99e8da1e26|https://proxy.transport.data.gouv.fr/resource/consolidation-nationale-irve-dynamique",
			SourceType:  "irve",
			Country:     "FR",
			PollCron:    "0 6 2 * *",  // monthly identity refresh (the 585 MB base)
			StatusCron:  "30 3 * * *", // daily — the dynamic file is rebuilt nightly (~00:45)
			Enabled:     true,
		},
		{
			// 🇪🇸 ES DGT/MITERD "electrolineras" — the Spanish NAP's AFIR
			// publication: open DATEX II v3 (no key, CC-BY), ~12,350 sites /
			// ~37,000 refill points / ~44,000 connectors, regenerated daily (~85 MB).
			// LOCATION-ONLY: no ad-hoc price and no status publication exists yet.
			// Same profile as Eco-Movement/Indigo, with two Spanish specifics the
			// shared reader now handles — coordinates on coordinatesForDisplay, and
			// the address as labelled free-text lines ("Dirección: …").
			ID:          "es-dgt",
			Name:        "DGT · Puntos de recarga (ES)",
			OCPIBaseURL: "https://infocar.dgt.es/datex2/v3/miterd/EnergyInfrastructureTablePublication/electrolineras.xml",
			SourceType:  "datex",
			Country:     "ES",
			PollCron:    "0 4 * * *", // daily, matching the publisher's 24h cadence
			StatusCron:  "0 4 * * *", // no live status in this publication
			Enabled:     true,        // open data, no key required
		},
		{
			// 🇵🇹 PT Mobi.E — Portugal's single national charging network, published
			// open (no key) on the NAP as an AFIR DATEX II table+status pair:
			// ~8,200 sites / ~20,300 refill points, with LIVE STATUS and per-point
			// AD-HOC PRICE. Mobi.E encodes the price differently from the other AFIR
			// publishers (a pricingPolicy + fee per electricEnergyMixOverride rather
			// than an energyRateUpdate), which the AFIR reader maps.
			// The table is 187 MB, so the reader's 1h in-process cache carries it
			// across the frequent status polls.
			ID:          "pt-mobie",
			Name:        "Mobi.E (PT)",
			OCPIBaseURL: "https://pgm.mobie.pt/integration/nap/evChargingInfra|https://pgm.mobie.pt/integration/nap/evActualStatus",
			SourceType:  "datex_afir",
			Country:     "PT",
			PollCron:    "0 4 * * *",    // daily price refresh
			StatusCron:  "*/15 * * * *", // availability every 15 min (41 MB status feed)
			Enabled:     true,           // open data, no key required
		},
		{
			// 🇫🇮 FI Fintraffic AFIR — Finland's NAP collects the CPOs' OCPI and
			// republishes it open (no key) as one national feed: ~3,800 locations /
			// ~19,900 EVSEs with LIVE STATUS and structured AD-HOC TARIFFS,
			// regenerated every minute. Prices are published net of VAT, so the
			// adapter grosses them up to what a driver pays (see internal/fintraffic).
			ID:          "fi-fintraffic",
			Name:        "Fintraffic AFIR (FI)",
			OCPIBaseURL: "https://afir.digitraffic.fi/api/charging-network/v1",
			SourceType:  "fintraffic",
			Country:     "FI",
			PollCron:    "0 4 * * *",    // daily price refresh
			StatusCron:  "*/10 * * * *", // availability: snapshots regenerate every minute; 10 min is plenty
			Enabled:     true,           // open data, no key required
		},
		{
			// 🇵🇱 PL UDT EIPA — the Polish national alternative-fuels register.
			// Not DATEX/OCPI: five static JSON files plus a dynamic one carrying
			// LIVE STATUS and AD-HOC PRICES. ~14,200 connectors on ~7,100 electric
			// stations. The account token is the last path segment of each file URL
			// (folded in by the adapter), and the register enforces 10 static
			// downloads/hour and 240 dynamic ones, hence the cached static half.
			// PRICES ARE IN PLN: they are stored as published, but a non-euro tariff
			// gets no euro comparable price, so Polish chargers rank as unpriced
			// until FX conversion exists (see processTariff).
			ID:          "pl-eipa",
			Name:        "UDT EIPA (PL)",
			OCPIBaseURL: "https://eipa.udt.gov.pl/reader/export-data",
			SourceType:  "eipa",
			TokenEnv:    "EIPA_TOKEN",
			Country:     "PL",
			PollCron:    "0 4 * * *",    // daily: refreshes the static half + prices
			StatusCron:  "*/10 * * * *", // 6 dynamic fetches/hour, well inside the 240/hour budget
			Enabled:     true,           // polled once EIPA_TOKEN is set
		},
		{
			// 🇦🇹 AT E-Control Ladestellenverzeichnis — the Austrian national
			// register, and the only new source carrying BOTH live status and a
			// structured ad-hoc price in euros (per kWh, per minute, start fee, and
			// a blocking fee with a grace threshold).
			// There is no bulk export: the API is walked operators → stations →
			// points. Measured live, that is 1,113 operators and 14,679 active
			// stations, so ONE pass is ~15,800 small requests — and status lives on
			// the point payload, so availability cannot be refreshed any cheaper.
			// Hence a daily price pass plus twice-daily availability (~3 crawls/day)
			// with a modest 4 concurrent requests: E-Control publishes no rate limit,
			// and until they tell us one, restraint beats being cut off. A bulk or
			// DATEX II export (their NAP record mentions DATEX) would fix this
			// properly — worth asking them for.
			// The key is sent as an "Apikey" header AND validated against the
			// request's Referer domain — see internal/econtrol.
			ID:          "at-econtrol",
			Name:        "E-Control Ladestellen (AT)",
			OCPIBaseURL: "https://api.e-control.at/charge/1.0",
			SourceType:  "econtrol",
			TokenEnv:    "ECONTROL_APIKEY",
			Country:     "AT",
			PollCron:    "0 4 * * *",    // daily price refresh (re-walks the whole tree)
			StatusCron:  "0 */12 * * *", // twice daily — each pass re-reads all ~15k stations
			Enabled:     true,           // polled once ECONTROL_APIKEY is set
		},
		{
			// 🇨🇭 CH SFOE ich-tanke-strom — the Swiss federal charging register.
			// Switzerland is outside AFIR, so this is OICP (Hubject) JSON rather
			// than DATEX II/OCPI: ~19,100 EVSEs of which ~14,400 are publicly
			// accessible (restricted-access points are dropped), with LIVE STATUS
			// and NO price. Open, no key; licence O-By-Ask (attribution).
			ID:          "ch-sfoe",
			Name:        "SFOE ich-tanke-strom (CH)",
			OCPIBaseURL: "https://data.geo.admin.ch/ch.bfe.ladestellen-elektromobilitaet/data/ch.bfe.ladestellen-elektromobilitaet.json|https://data.geo.admin.ch/ch.bfe.ladestellen-elektromobilitaet/status/ch.bfe.ladestellen-elektromobilitaet.json",
			SourceType:  "oicp",
			Country:     "CH",
			PollCron:    "0 5 * * *",    // daily identity refresh (no price to poll)
			StatusCron:  "*/15 * * * *", // availability every 15 min (1.2 MB + 0.2 MB)
			Enabled:     true,           // open data, no key required
		},
		{
			// 🇧🇪 Group Indigo (parking operator) — open static DATEX II (schema 3)
			// on the BE NAP. LOCATION-ONLY (no price, no live status): ~2,300
			// connectors in Indigo car parks, mostly 22 kW AC. Same DATEX II profile
			// as Eco-Movement, parsed by the shared `datex` reader. Open, no key.
			ID:          "indigo",
			Name:        "Group Indigo (BE)",
			OCPIBaseURL: "https://transportdata.be/dataset/27f1357d-71ee-48cb-84a1-96f3f4f034b8/resource/d4bc8ddd-c80f-4330-98e5-d86e5b2147c3/download/indigo-data-evcharging-static-datexii.xml",
			SourceType:  "datex",
			Country:     "BE",
			PollCron:    "0 5 * * *",
			StatusCron:  "0 5 * * *", // no live status; daily refresh is plenty
			Enabled:     true,        // open data, no key required
		},
		{
			// 🇧🇪 DATEX II aggregator (~20 networks, ~35,800 connectors). Validated
			// live: it carries locations + connector type + power, but NO price and
			// NO live status, and the response is ~31 MB, so poll it at most daily.
			// The NAP token authenticates via a ?token= query param (not a Bearer
			// header); feedFor folds the resolved token (here from ECOMOVEMENT_TOKEN)
			// into the URL. Coverage-only, but enabled for reach.
			ID:          "ecomovement",
			Name:        "Eco-Movement (NAP aggregator)",
			OCPIBaseURL: "https://api.eco-movement.com/api/nap/datexii/locations",
			SourceType:  "datex",
			TokenEnv:    "ECOMOVEMENT_TOKEN",
			Country:     "BE",
			PollCron:    "0 5 * * *",
			StatusCron:  "30 5 * * *", // daily; no live status in this feed
			Enabled:     true,
		},
	}
}
