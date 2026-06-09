-- Optional demo data so the API returns something before a real OCPI key is
-- available. Apply with:  make demo-seed   (or psql -f db/demo_seed.sql)
TRUNCATE tariff_version, charger_status, charger, ingest_run, cpo RESTART IDENTITY CASCADE;

INSERT INTO cpo (id, name, ocpi_base_url, enabled)
VALUES ('demo', 'Demo CPO', 'http://demo/', false);

WITH c AS (
    INSERT INTO charger (cpo_id, evse_uid, connector_id, geom, power_kw, plug_type, current_type, name, address)
    VALUES
        ('demo','E1','1', ST_SetSRID(ST_MakePoint(3.7250, 51.0543),4326)::geography, 22.1,'IEC_62196_T2','AC','Markt Charging','9000 Gent'),
        ('demo','E2','1', ST_SetSRID(ST_MakePoint(3.7268, 51.0551),4326)::geography, 50.0,'IEC_62196_T2_COMBO','DC','Korenmarkt Fast','9000 Gent')
    RETURNING id, current_type
)
INSERT INTO tariff_version (charger_id, tariff_hash, price_components, comparable_price_eur, currency, observed_from)
SELECT id,
       'demo-' || id,
       '{"currency":"EUR"}'::jsonb,
       CASE WHEN current_type='DC' THEN 17.5000 ELSE 13.7500 END,
       'EUR', now()
FROM c;

INSERT INTO charger_status (charger_id, status, available_count)
SELECT id, 'AVAILABLE', 1 FROM charger;
