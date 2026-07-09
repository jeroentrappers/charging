package store

import (
	"context"
	"time"
)

// DatexSubscription is one external consumer's registered push callback.
type DatexSubscription struct {
	ID        int64
	URL       string
	Encoding  string // "xml" | "json"
	PushToken string // bearer we send on each push; may be empty
}

// UpsertDatexSubscription records a verified subscription, refreshing it if the
// same (url, encoding) already exists. Returns the row id. manageSecret is
// stored so only the registrant can later delete it.
func (s *Store) UpsertDatexSubscription(ctx context.Context, url, encoding, pushToken, manageSecret string, now time.Time) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO datex_subscription (callback_url, encoding, push_token, manage_secret, active, verified_at)
		VALUES ($1, $2, $3, $4, true, $5)
		ON CONFLICT (callback_url, encoding) DO UPDATE
		SET push_token = EXCLUDED.push_token,
		    manage_secret = EXCLUDED.manage_secret,
		    active = true,
		    verified_at = EXCLUDED.verified_at
		RETURNING id`,
		url, encoding, pushToken, manageSecret, now).Scan(&id)
	return id, err
}

// ListActiveDatexSubscriptions returns every active subscription.
func (s *Store) ListActiveDatexSubscriptions(ctx context.Context) ([]DatexSubscription, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, callback_url, encoding, push_token
		FROM datex_subscription
		WHERE active
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatexSubscription
	for rows.Next() {
		var d DatexSubscription
		if err := rows.Scan(&d.ID, &d.URL, &d.Encoding, &d.PushToken); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDatexSubscription removes a subscription, but only when manageSecret
// matches the one issued at registration. Returns true if a row was deleted.
func (s *Store) DeleteDatexSubscription(ctx context.Context, id int64, manageSecret string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		DELETE FROM datex_subscription WHERE id = $1 AND manage_secret = $2`,
		id, manageSecret)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
