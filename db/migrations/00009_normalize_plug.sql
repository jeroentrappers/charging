-- +goose Up
-- +goose StatementBegin
-- Normalize connector-standard spellings to canonical OCPI values (the ingester
-- now does this via model.NormalizePlug; this backfills existing rows). Keyed on
-- the case/underscore-stripped form so both "IEC_62196_T2" and "iec62196T2" map.
UPDATE charger SET plug_type = CASE upper(replace(replace(plug_type,'_',''),'-',''))
  WHEN 'IEC62196T2'       THEN 'IEC_62196_T2'
  WHEN 'IEC62196T2COMBO'  THEN 'IEC_62196_T2_COMBO'
  WHEN 'IEC62196T1'       THEN 'IEC_62196_T1'
  WHEN 'IEC62196T1COMBO'  THEN 'IEC_62196_T1_COMBO'
  WHEN 'IEC62196T3C'      THEN 'IEC_62196_T3C'
  WHEN 'IEC62196T3A'      THEN 'IEC_62196_T3A'
  WHEN 'CHADEMO'          THEN 'CHADEMO'
  WHEN 'DOMESTICF'        THEN 'DOMESTIC_F'
  WHEN 'DOMESTICE'        THEN 'DOMESTIC_E'
  WHEN 'TESLAS'           THEN 'TESLA_S'
  WHEN 'TESLAR'           THEN 'TESLA_R'
  ELSE plug_type END
WHERE plug_type IS NOT NULL AND plug_type <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: canonicalisation is not reversible (original spellings are lost).
SELECT 1;
-- +goose StatementEnd
