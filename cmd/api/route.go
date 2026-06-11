package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/appmire/charging/internal/routing"
	"github.com/appmire/charging/internal/store"
)

// ---- GET /chargers/along-route (corridor search) ----

type alongRouteIn struct {
	FromLat        float64 `query:"from_lat" required:"true" doc:"Trip start latitude"`
	FromLon        float64 `query:"from_lon" required:"true" doc:"Trip start longitude"`
	ToLat          float64 `query:"to_lat" required:"true" doc:"Trip end latitude"`
	ToLon          float64 `query:"to_lon" required:"true" doc:"Trip end longitude"`
	Buffer         float64 `query:"buffer" default:"2500" minimum:"100" maximum:"20000" doc:"Max metres a charger may sit from the route"`
	MinPower       float64 `query:"min_power" doc:"Only chargers rated at least this many kW"`
	Plug           string  `query:"plug" doc:"OCPI connector standard"`
	Available      bool    `query:"available" doc:"Only chargers currently reported free"`
	IncludePrivate bool    `query:"include_private" doc:"Include private (home / peer-to-peer) chargers"`
	Limit          int     `query:"limit" default:"60" minimum:"1" maximum:"300" doc:"Maximum candidates to return"`
}

type alongRouteOut struct {
	Body struct {
		Route   *routing.Route        `json:"route"` // polyline + total distance/duration
		Count   int                   `json:"count"`
		Results []store.NearbyCharger `json:"results"` // DistanceM = off-route distance
	}
}

// opAlongRoute routes from->to (self-hosted OSRM), then returns chargers within
// `buffer` of the line with their structured tariffs so the client prices +
// ranks by price + deviation. DistanceM on each result is the off-route distance.
func (s *server) opAlongRoute(ctx context.Context, in *alongRouteIn) (*alongRouteOut, error) {
	if s.router == nil {
		return nil, huma.Error503ServiceUnavailable("route search is not configured")
	}
	rt, err := s.router.Route(ctx, routing.LatLon{Lat: in.FromLat, Lon: in.FromLon}, routing.LatLon{Lat: in.ToLat, Lon: in.ToLon})
	if err != nil {
		s.log.Warn("route", "err", err)
		return nil, huma.Error502BadGateway("could not compute a route")
	}

	res, err := s.st.ChargersAlongRoute(ctx, lineWKT(rt.Points), in.Buffer, store.NearbyQuery{
		MinPowerKW: in.MinPower, PlugType: in.Plug, OnlyAvail: in.Available,
		IncludePrivate: in.IncludePrivate, StaleAfter: s.staleAfter, Limit: in.Limit,
	})
	if err != nil {
		s.log.Error("along-route query", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	s.attachReports(ctx, res)
	if res == nil {
		res = []store.NearbyCharger{}
	}
	out := &alongRouteOut{}
	out.Body.Route = rt
	out.Body.Count = len(res)
	out.Body.Results = res
	return out, nil
}

// lineWKT builds a WKT LINESTRING (lon lat order) from the route points. A
// single-point route degenerates to a tiny segment so ST_GeomFromText is happy.
func lineWKT(pts []routing.LatLon) string {
	if len(pts) == 1 {
		pts = append(pts, pts[0])
	}
	var b strings.Builder
	b.WriteString("LINESTRING(")
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%f %f", p.Lon, p.Lat)
	}
	b.WriteByte(')')
	return b.String()
}
