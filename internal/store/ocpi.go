package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// SetOCPIIncomingToken stores the token a CPO will present when pushing to us
// (Token B, issued during the credentials handshake).
func (s *Store) SetOCPIIncomingToken(ctx context.Context, id, token string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE cpo SET ocpi_token_in = NULLIF($2,'') WHERE id=$1`, id, token)
	return tag.RowsAffected() > 0, err
}

// CPOByIncomingToken resolves a pushed-request token to its source id.
func (s *Store) CPOByIncomingToken(ctx context.Context, token string) (string, bool, error) {
	if token == "" {
		return "", false, nil
	}
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id FROM cpo WHERE ocpi_token_in = $1`, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}
