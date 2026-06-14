package store

import "context"

// RecomputeSuperseded refreshes charger.superseded for cross-source dedup: a
// blanket location-only register charger (bnetza/irve) is marked superseded
// when a richer priced source covers the same spot (within 40 m, same
// normalised plug). The bulk export filters these out, so the open dumps don't
// double-count chargers that several feeds cover. Returns the number now
// superseded. Driven from the (smaller) richer set for speed; run periodically
// (before each full export), not per-request.
func (s *Store) RecomputeSuperseded(ctx context.Context) (int64, error) {
	if _, err := s.Pool.Exec(ctx, `UPDATE charger SET superseded = false WHERE superseded`); err != nil {
		return 0, err
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE charger reg SET superseded = true
		FROM charger rich
		JOIN cpo rp ON rp.id = rich.cpo_id
		WHERE rp.source_type NOT IN ('bnetza','irve')
		  AND reg.cpo_id IN (SELECT id FROM cpo WHERE source_type IN ('bnetza','irve'))
		  AND reg.id <> rich.id
		  AND ST_DWithin(reg.geom, rich.geom, 40)
		  AND upper(replace(COALESCE(reg.plug_type,''),'_','')) = upper(replace(COALESCE(rich.plug_type,''),'_',''))`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
