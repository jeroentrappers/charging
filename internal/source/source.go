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

// Seeds returns the known Belgian NAP charging sources to register on startup.
// EnergyVision is disabled until a free OCPI key is obtained
// (email myevplatform@energyvision.be) and ENERGYVISION_TOKEN is set.
func Seeds() []store.CPO {
	return []store.CPO{
		{
			ID:          "energyvision",
			Name:        "EnergyVision",
			OCPIBaseURL: "https://ocpi.energyvision.be/cpo/2.1.1/",
			OCPIVersion: "2.1.1",
			TokenEnv:    "ENERGYVISION_TOKEN",
			PollCron:    "0 4 * * *", // daily 04:00; price changes are rare
			Enabled:     false,       // flip to true once a token is available
		},
	}
}
