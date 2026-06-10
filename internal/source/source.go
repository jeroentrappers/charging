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
			ID:          "energyvision",
			Name:        "EnergyVision",
			OCPIBaseURL: "https://ocpi.energyvision.be/cpo/2.1.1/",
			OCPIVersion: "2.1.1",
			TokenEnv:    "ENERGYVISION_TOKEN",
			PollCron:    "0 4 * * *",   // daily 04:00; price changes are rare
			StatusCron:  "*/3 * * * *", // availability every 3 min
			Enabled:     false,         // ready for the current client; needs a token
		},
		{
			// OCPI 2.2.1 — the client now supports 2.2.1; enable once a token is set.
			ID:          "tesla",
			Name:        "Tesla Belgium",
			OCPIBaseURL: "https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/",
			OCPIVersion: "2.2.1",
			TokenEnv:    "TESLA_TOKEN",
			PollCron:    "0 4 * * *",
			StatusCron:  "*/5 * * * *", // Tesla refreshes every 5 min
			Enabled:     false,
		},
		{
			// Open static OCPI 2.2.1 files (no token) — real data available now.
			// OCPIBaseURL is the directory hosting locations.json + tariffs.json.
			ID:          "road",
			Name:        "Road",
			OCPIBaseURL: "https://roaming.road.io/files/9ef09c78-2666-418a-aa45-4f2261e2e305",
			OCPIVersion: "2.2.1",
			SourceType:  "ocpi_file",
			PollCron:    "0 5 * * *",    // daily price refresh
			StatusCron:  "*/15 * * * *", // availability every 15 min (5 MB file)
			Enabled:     true,           // open data, no key required
		},
		{
			// DATEX II aggregator (~20 networks, ~36k connectors). Validated
			// against the live feed: it carries locations + connector type +
			// power, but NO price and NO live status, and the response is ~31 MB,
			// so poll it at most daily. The NAP token goes in the URL query param
			// (the feed is open); set the full URL incl. ?token=… via the CLI:
			//   chargingctl sources add ecomovement --type datex \
			//     --url "https://api.eco-movement.com/api/nap/datexii/locations?token=…"
			// Disabled by default: it's coverage-only (no price), so enable
			// deliberately. OCPIBaseURL is the feed URL (token query param).
			ID:          "ecomovement",
			Name:        "Eco-Movement (NAP aggregator)",
			OCPIBaseURL: "https://api.eco-movement.com/api/nap/datexii/locations",
			SourceType:  "datex",
			PollCron:    "0 5 * * *",
			StatusCron:  "30 5 * * *", // daily; no live status in this feed
			Enabled:     false,
		},
	}
}
