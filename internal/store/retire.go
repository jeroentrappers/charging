package store

import "context"

// CountActiveChargers returns the number of non-retired chargers for a source.
// Used as a safety denominator before pruning (so a truncated feed can't wipe a
// source).
func (s *Store) CountActiveChargers(ctx context.Context, cpoID string) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM charger WHERE cpo_id=$1 AND NOT retired`, cpoID).Scan(&n)
	return n, err
}

// RetireAbsentChargers reconciles a source's chargers against the full set seen
// in the latest pass: chargers not present are marked retired (dropped from the
// feeds and app, history kept), and any previously-retired charger that
// reappears is revived. seenEVSE[i]/seenConn[i] are the (evse_uid, connector_id)
// of one seen connector. Returns how many were newly retired / revived.
//
// The seen set is materialized in a CTE so the anti-/semi-joins are hash joins
// (not a per-row array scan), which stays fast at tens of thousands of rows.
func (s *Store) RetireAbsentChargers(ctx context.Context, cpoID string, seenEVSE, seenConn []string) (retired, revived int, err error) {
	tag, err := s.Pool.Exec(ctx, `
		WITH seen(evse_uid, connector_id) AS (
			SELECT DISTINCT * FROM unnest($2::text[], $3::text[])
		)
		UPDATE charger c SET retired = true, retired_at = now()
		WHERE c.cpo_id = $1 AND NOT c.retired
		  AND NOT EXISTS (
			SELECT 1 FROM seen s
			WHERE s.evse_uid = c.evse_uid AND s.connector_id = c.connector_id)`,
		cpoID, seenEVSE, seenConn)
	if err != nil {
		return 0, 0, err
	}
	retired = int(tag.RowsAffected())

	tag, err = s.Pool.Exec(ctx, `
		WITH seen(evse_uid, connector_id) AS (
			SELECT DISTINCT * FROM unnest($2::text[], $3::text[])
		)
		UPDATE charger c SET retired = false, retired_at = NULL
		WHERE c.cpo_id = $1 AND c.retired
		  AND EXISTS (
			SELECT 1 FROM seen s
			WHERE s.evse_uid = c.evse_uid AND s.connector_id = c.connector_id)`,
		cpoID, seenEVSE, seenConn)
	if err != nil {
		return retired, 0, err
	}
	revived = int(tag.RowsAffected())
	return retired, revived, nil
}
