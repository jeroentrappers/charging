-- Optional demo data so the API returns something before a real OCPI key is
-- available. Apply with:  make demo-seed   (or psql -f db/demo_seed.sql)
--
-- The comparable_prices below are illustrative but internally consistent:
--   AC charger: pure 0.40 €/kWh  -> power tier doesn't change price (no time fee)
--   DC charger: 0.59 €/kWh + 0.35 session + 6 €/h -> faster DC (300) beats 150
--     because the per-hour component runs for less time (charging-curve aware).
TRUNCATE tariff_version, charger_status, charger, ingest_run, cpo RESTART IDENTITY CASCADE;

INSERT INTO cpo (id, name, ocpi_base_url, enabled)
VALUES ('demo', 'Demo CPO', 'http://demo/', false);

-- Charger 1: AC 22 kW (Type 2), Gent Markt.
WITH c AS (
    INSERT INTO charger (cpo_id, evse_uid, connector_id, geom, power_kw, plug_type, current_type, name, address)
    VALUES ('demo','E1','1', ST_SetSRID(ST_MakePoint(3.7250, 51.0543),4326)::geography, 22.0,'IEC_62196_T2','AC','Markt Charging','9000 Gent')
    RETURNING id
)
INSERT INTO tariff_version (charger_id, tariff_hash, price_components, comparable_price_eur, comparable_prices, currency, observed_from)
SELECT id, 'demo-ac',
    '{"currency":"EUR","elements":[{"price_components":[{"type":"ENERGY","price":0.40,"step_size":1}]}]}'::jsonb,
    18.8800,
    '{"topup100_ac11":8.09,"topup100_ac22":8.09,"charge1080_ac11":18.88,"charge1080_ac22":18.88,"urban_ac22":3.24,"overnight_ac11":24.27}'::jsonb,
    'EUR', now()
FROM c;

-- Charger 2: DC 300 kW (CCS HPC), Gent Korenmarkt.
WITH c AS (
    INSERT INTO charger (cpo_id, evse_uid, connector_id, geom, power_kw, plug_type, current_type, name, address)
    VALUES ('demo','E2','1', ST_SetSRID(ST_MakePoint(3.7268, 51.0551),4326)::geography, 300.0,'IEC_62196_T2_COMBO','DC','Korenmarkt Fast','9000 Gent')
    RETURNING id
)
INSERT INTO tariff_version (charger_id, tariff_hash, price_components, comparable_price_eur, comparable_prices, currency, observed_from)
SELECT id, 'demo-dc',
    '{"currency":"EUR","elements":[{"price_components":[{"type":"ENERGY","price":0.59,"step_size":1},{"type":"FLAT","price":0.35,"step_size":1},{"type":"TIME","price":6.00,"step_size":1}]}]}'::jsonb,
    27.9300,
    '{"topup100_dc150":12.50,"topup100_dc300":12.13,"charge1080_dc150":29.15,"charge1080_dc300":28.20}'::jsonb,
    'EUR', now()
FROM c;

INSERT INTO charger_status (charger_id, status, available_count)
SELECT id, 'AVAILABLE', 1 FROM charger;
