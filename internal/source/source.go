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

// Client builds an OCPI client for this source.
func (s Source) Client() *ocpi.Client {
	return ocpi.New(s.CPO.OCPIBaseURL, s.Token)
}

// HasToken reports whether a usable token was resolved.
func (s Source) HasToken() bool { return s.Token != "" }

// Resolve turns CPO registry rows into sources, reading each one's token from
// the environment variable named by CPO.TokenEnv.
func Resolve(cpos []store.CPO) []Source {
	out := make([]Source, 0, len(cpos))
	for _, c := range cpos {
		tok := ""
		if c.TokenEnv != "" {
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
			// OCPI 2.2.1 — enable once the client supports 2.2.1 (auth + tariffs).
			ID:          "tesla",
			Name:        "Tesla Belgium",
			OCPIBaseURL: "https://charging-roaming-data.tesla.com/ocpi/cpo/2.2.1/",
			OCPIVersion: "2.2.1",
			TokenEnv:    "TESLA_TOKEN",
			PollCron:    "0 4 * * *",
			StatusCron:  "*/5 * * * *", // Tesla refreshes every 5 min
			Enabled:     false,
		},
	}
}
