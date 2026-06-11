// Package routing turns an origin + destination into a driving route, so the
// app can find chargers along a trip (not just around a point). It's an
// interface with an OSRM-backed implementation; swap the impl to change engines.
package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// LatLon is a WGS84 coordinate.
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Route is a driving route: the polyline plus total distance/duration.
type Route struct {
	Points    []LatLon `json:"points"`
	DistanceM float64  `json:"distance_m"`
	DurationS float64  `json:"duration_s"`
}

// Router computes a driving route between two points.
type Router interface {
	Route(ctx context.Context, from, to LatLon) (*Route, error)
}

// OSRM talks to an osrm-routed HTTP server (self-hosted). BaseURL is e.g.
// http://osrm:5000 (no trailing slash needed).
type OSRM struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns an OSRM router with a sane default timeout.
func New(baseURL string) *OSRM {
	return &OSRM{BaseURL: baseURL, HTTP: &http.Client{Timeout: 8 * time.Second}}
}

type osrmResp struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][2]float64 `json:"coordinates"` // [lon, lat]
		} `json:"geometry"`
	} `json:"routes"`
}

// Route fetches the driving route from->to with full GeoJSON geometry.
func (o *OSRM) Route(ctx context.Context, from, to LatLon) (*Route, error) {
	// OSRM coordinate order is lon,lat.
	path := fmt.Sprintf("%s/route/v1/driving/%f,%f;%f,%f", o.BaseURL, from.Lon, from.Lat, to.Lon, to.Lat)
	q := url.Values{"overview": {"full"}, "geometries": {"geojson"}, "alternatives": {"false"}, "steps": {"false"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osrm: status %d", resp.StatusCode)
	}
	var r osrmResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Code != "Ok" || len(r.Routes) == 0 {
		return nil, fmt.Errorf("osrm: no route (%s)", r.Code)
	}
	rt := r.Routes[0]
	out := &Route{DistanceM: rt.Distance, DurationS: rt.Duration, Points: make([]LatLon, 0, len(rt.Geometry.Coordinates))}
	for _, c := range rt.Geometry.Coordinates {
		out.Points = append(out.Points, LatLon{Lat: c[1], Lon: c[0]})
	}
	return out, nil
}
