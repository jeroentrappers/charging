package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LiveRef is what the on-demand live-status endpoint needs about a charger: its
// Monta identity (to query upstream) plus the latest stored status to fall back
// on.
type LiveRef struct {
	ID             int64
	CPOID          string
	EVSEUID        string
	PowerKW        float64
	CurrentType    string
	Status         string
	AvailableCount int
	StatusAt       *time.Time
}

// GetLiveRef loads a charger's identity and last-known status by id.
func (s *Store) GetLiveRef(ctx context.Context, id int64) (LiveRef, bool, error) {
	var r LiveRef
	err := s.Pool.QueryRow(ctx, `
		SELECT c.id, c.cpo_id, c.evse_uid,
		       COALESCE(c.power_kw,0)::float8, COALESCE(c.current_type,''),
		       COALESCE(st.status,''), COALESCE(st.available_count,0), st.updated_at
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		WHERE c.id = $1`, id).Scan(
		&r.ID, &r.CPOID, &r.EVSEUID, &r.PowerKW, &r.CurrentType,
		&r.Status, &r.AvailableCount, &r.StatusAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LiveRef{}, false, nil
		}
		return LiveRef{}, false, err
	}
	return r, true, nil
}
