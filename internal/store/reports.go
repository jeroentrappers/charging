package store

import (
	"context"
	"encoding/json"

	"github.com/appmire/charging/internal/report"
)

// AddReport records (or refreshes) one client's structured report for a charger.
// The composite PK dedupes: a client holds at most one report of each type per
// charger; re-submitting updates the value and recency.
func (s *Store) AddReport(ctx context.Context, chargerID int64, typ, clientHash string, value json.RawMessage) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO charger_report (charger_id, type, client_hash, value, created_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (charger_id, type, client_hash)
		DO UPDATE SET value = EXCLUDED.value, created_at = now()`,
		chargerID, typ, clientHash, value)
	return err
}

// ChargerExists reports whether a charger id is known (for a clean 404).
func (s *Store) ChargerExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM charger WHERE id=$1)`, id).Scan(&exists)
	return exists, err
}

// ReportsRaw returns a charger's recent raw reports (last 90 days — the longest
// type TTL); per-type TTL + opposite suppression are applied by report.Aggregate.
func (s *Store) ReportsRaw(ctx context.Context, chargerID int64) ([]report.Raw, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT type, value, created_at
		FROM charger_report
		WHERE charger_id = $1 AND created_at > now() - interval '90 days'
		ORDER BY created_at`, chargerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []report.Raw
	for rows.Next() {
		var r report.Raw
		if err := rows.Scan(&r.Type, &r.Value, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReportsForIDs returns recent raw reports for many chargers at once, grouped by
// charger id — used to flag/de-prioritise candidates in the cheapest list.
func (s *Store) ReportsForIDs(ctx context.Context, ids []int64) (map[int64][]report.Raw, error) {
	out := map[int64][]report.Raw{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT charger_id, type, value, created_at
		FROM charger_report
		WHERE charger_id = ANY($1) AND created_at > now() - interval '90 days'
		ORDER BY created_at`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var r report.Raw
		if err := rows.Scan(&id, &r.Type, &r.Value, &r.CreatedAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], r)
	}
	return out, rows.Err()
}

// DeleteReports removes all reports for a charger (admin moderation). Returns the
// number of rows removed.
func (s *Store) DeleteReports(ctx context.Context, chargerID int64) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM charger_report WHERE charger_id=$1`, chargerID)
	return tag.RowsAffected(), err
}
