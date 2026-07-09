package export

// Bridge from our export rows to the DATEX II AFIR publish inputs. Grouping
// mirrors the OCPI builder: one site + one station per cpo_id|evse_uid, one
// refillPoint per connector row. The refillPoint id is the charger DB id, so the
// status publication (built from the availability delta, keyed by the same id)
// joins back to the table.

import (
	"strconv"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/store"
)

// BuildAFIRTable groups export rows into DATEX II AFIR publish sites (with
// ad-hoc pricing from each row's structured tariff).
func BuildAFIRTable(rows []store.ExportCharger) []datex.PublishSite {
	sites := make([]datex.PublishSite, 0)
	index := map[string]int{} // cpo_id|evse_uid -> sites index
	for i := range rows {
		r := &rows[i]
		key := r.CPOID + "|" + r.EVSEUID
		si, ok := index[key]
		if !ok {
			si = len(sites)
			index[key] = si
			sites = append(sites, datex.PublishSite{
				ID:         key,
				Name:       r.Name,
				Lat:        r.Lat,
				Lon:        r.Lon,
				PostalCode: r.PostalCode,
				City:       r.City,
				Street:     r.Address,
				Stations:   []datex.PublishStation{{ID: key}},
			})
		}
		rp := datex.PublishRefillPoint{
			ID:            strconv.FormatInt(r.ID, 10),
			CurrentType:   r.CurrentType,
			ConnectorType: r.PlugType,
			PowerKW:       r.PowerKW,
			Rate:          buildPublishRate(r),
		}
		st := &sites[si].Stations[0]
		st.RefillPoints = append(st.RefillPoints, rp)
	}
	return sites
}

// buildPublishRate turns the row's structured tariff into a DATEX ad-hoc rate,
// or nil when there is no usable price.
func buildPublishRate(r *store.ExportCharger) *datex.PublishRate {
	t := parseTariff(r.Components)
	if t == nil || len(t.Elements) == 0 {
		return nil
	}
	var prices []datex.PublishPrice
	for _, el := range t.Elements {
		for _, pc := range el.PriceComponents {
			prices = append(prices, datex.PublishPrice{Type: pc.Type, Value: pc.Price})
		}
	}
	if len(prices) == 0 {
		return nil
	}
	return &datex.PublishRate{
		Currency:    t.Currency,
		LastUpdated: t.LastUpdated,
		Prices:      prices,
	}
}

// BuildAFIRStatus turns the availability delta into DATEX II status entries,
// keyed by the charger DB id (== each table refillPoint id).
func BuildAFIRStatus(avail []store.AvailabilitySnapshot) []datex.PublishStatus {
	out := make([]datex.PublishStatus, 0, len(avail))
	for _, a := range avail {
		if a.Status == "" {
			continue
		}
		out = append(out, datex.PublishStatus{
			RefillPointID: strconv.FormatInt(a.ID, 10),
			Status:        a.Status,
		})
	}
	return out
}
