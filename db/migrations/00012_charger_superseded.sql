-- +goose Up
-- Cross-source dedup for the bulk export: a blanket location-only register
-- charger (bnetza/irve) is "superseded" when a richer priced source covers the
-- same spot. Precomputed by RecomputeSuperseded (the per-row spatial subquery is
-- too slow to run over the whole table at export time).
ALTER TABLE charger ADD COLUMN superseded boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE charger DROP COLUMN superseded;
