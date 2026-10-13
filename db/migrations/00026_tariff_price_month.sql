-- +goose NO TRANSACTION
-- CREATE INDEX CONCURRENTLY cannot run inside a transaction. tariff_version is
-- tens of millions of rows and ingest writes to it every minute, so a plain
-- CREATE INDEX would block every tariff write for the length of the build.

-- +goose Up
-- Monthly rollup behind /stats/price-trend. The trend used to be a range join of
-- the month buckets against the full SCD2 history, recomputed on demand: ~8s at
-- launch, minutes once the history passed ~30M versions, so it could no longer
-- finish inside the request or the cache refresh.
--
-- A version counts toward every month its validity window touches, exactly as
-- before. Versions only ever open and close at now(), so the set touching a month
-- is frozen once that month has ended: a month computed after its end (computed_at
-- >= month + 1 month) is final and never recomputed. Only the open month(s) are.
-- See store.RefreshPriceMonths.
CREATE TABLE IF NOT EXISTS tariff_price_month (
    month       date PRIMARY KEY,           -- first day of the month
    sum_eur     numeric NOT NULL,           -- sum of comparable_price_eur
    n           bigint NOT NULL,            -- versions touching the month
    computed_at timestamptz NOT NULL
);

-- The incremental refresh reads only versions still open or closed since the
-- first non-final month (IS NULL OR >= $1). btree indexes NULLs, so one index
-- serves both arms.
CREATE INDEX CONCURRENTLY IF NOT EXISTS tariff_priced_to_ix
    ON tariff_version (observed_to) WHERE comparable_price_eur IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS tariff_priced_to_ix;
DROP TABLE IF EXISTS tariff_price_month;
