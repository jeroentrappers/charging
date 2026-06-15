-- +goose Up
-- Overlay push feeds (e.g. eliso) match chargers by EVSE id alone, without the
-- CPO — the locations are seeded under a broker/aggregator CPO. The existing
-- unique index is (cpo_id, evse_uid, connector_id), whose leading column is
-- cpo_id, so an evse_uid-only lookup degrades to a full index scan (~23ms over
-- 500k+ rows). This dedicated index makes ChargersForEVSEAny a point lookup.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS charger_evse_uid_ix ON charger (evse_uid);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS charger_evse_uid_ix;
-- +goose StatementEnd
