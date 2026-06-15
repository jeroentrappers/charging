package store

import (
	"math"
	"sort"
)

// clusterCoordDecimals snaps coordinates to ~11 m (4 decimal places) so all the
// chargers in one car park/site collapse to a single cluster key.
const clusterCoordDecimals = 1e4

// memberState classifies a charger's live availability: "free" when its status
// is fresh and reports a free connector, "busy" when fresh and none free, else
// "unknown" (no reading or stale).
func memberState(c NearbyCharger) string {
	if c.Stale || c.StatusAt == nil {
		return "unknown"
	}
	if c.Available > 0 {
		return "free"
	}
	return "busy"
}

// betterRep reports whether a should represent a cluster over b: prefer a priced
// charger, then the cheaper one, then the nearer one. The representative carries
// the cluster's headline price + structured tariff (co-located same-power
// chargers share a tariff, so this is the group's price).
func betterRep(a, b NearbyCharger) bool {
	ap, bp := a.PriceEUR, b.PriceEUR
	if (ap == nil) != (bp == nil) {
		return ap != nil
	}
	if ap != nil && bp != nil && *ap != *bp {
		return *ap < *bp
	}
	return a.DistanceM < b.DistanceM
}

// ClusterByLocationPower groups chargers at the same approximate location, of the
// same power level and operator, into one representative row carrying the group's
// availability counts (free/busy) and its members for drill-down — so a site with
// 100 identical chargers shows as one entry instead of 100. Single-charger groups
// pass through unchanged (GroupTotal stays 0).
//
// Clusters are ordered priced-first, then cheapest, then nearest, and truncated
// to limit. When preferPriced is set (corridor search) and any priced cluster
// exists, unpriced clusters are dropped entirely; otherwise they're kept (just
// ranked last) so the result is never empty.
func ClusterByLocationPower(rows []NearbyCharger, limit int, preferPriced bool) []NearbyCharger {
	if limit <= 0 {
		limit = 100
	}
	type key struct {
		lat, lon, pow int64
		cpo           string
	}
	order := make([]key, 0, len(rows))
	rep := map[key]NearbyCharger{}
	free := map[key]int{}
	busy := map[key]int{}
	members := map[key][]ClusterMember{}

	for _, r := range rows {
		k := key{
			lat: int64(math.Round(r.Lat * clusterCoordDecimals)),
			lon: int64(math.Round(r.Lon * clusterCoordDecimals)),
			pow: int64(math.Round(r.PowerKW)),
			cpo: r.CPOID,
		}
		st := memberState(r)
		switch st {
		case "free":
			free[k]++
		case "busy":
			busy[k]++
		}
		members[k] = append(members[k], ClusterMember{ID: r.ID, EVSEUID: r.EVSEUID, Status: st})
		if cur, ok := rep[k]; !ok {
			rep[k] = r
			order = append(order, k)
		} else if betterRep(r, cur) {
			rep[k] = r
		}
	}

	out := make([]NearbyCharger, 0, len(order))
	for _, k := range order {
		c := rep[k]
		total := len(members[k])
		c.GroupAvailable = free[k]
		c.GroupBusy = busy[k]
		// Surface the group's free count through Available so existing availability
		// logic (free when >0) reflects the whole cluster.
		c.Available = free[k]
		if total > 1 {
			c.GroupTotal = total
			c.Members = members[k]
		}
		out = append(out, c)
	}

	sort.SliceStable(out, func(i, j int) bool { return betterRep(out[i], out[j]) })

	if preferPriced {
		hasPriced := false
		for _, c := range out {
			if c.PriceEUR != nil {
				hasPriced = true
				break
			}
		}
		if hasPriced {
			kept := out[:0]
			for _, c := range out {
				if c.PriceEUR != nil {
					kept = append(kept, c)
				}
			}
			out = kept
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
