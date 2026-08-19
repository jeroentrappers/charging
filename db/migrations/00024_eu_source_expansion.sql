-- +goose Up
-- +goose StatementBegin
-- EU expansion: ES / PT / FI / CH / PL / AT join the registry as new seeds
-- (SeedCPO inserts them on the next start; PL and AT only poll once EIPA_TOKEN
-- and ECONTROL_APIKEY are set), but the existing FR row has to be migrated in
-- place — France now publishes a consolidated national DYNAMIC file, the only
-- availability it offers. Point the source at "<static>|<dynamic>" and move the
-- availability pass to daily, just after the publisher's nightly (~00:45)
-- rebuild. The price pass stays monthly: the static base is 585 MB and France
-- still publishes no structured price. Guarded on the current single URL so an
-- operator-tuned row is left alone.
UPDATE cpo SET
    ocpi_base_url = 'https://www.data.gouv.fr/api/1/datasets/r/7eee8f09-5d1b-4f48-a304-5e99e8da1e26|https://proxy.transport.data.gouv.fr/resource/consolidation-nationale-irve-dynamique',
    status_cron   = '30 3 * * *'
WHERE id = 'irve'
  AND ocpi_base_url = 'https://www.data.gouv.fr/api/1/datasets/r/7eee8f09-5d1b-4f48-a304-5e99e8da1e26';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE cpo SET
    ocpi_base_url = 'https://www.data.gouv.fr/api/1/datasets/r/7eee8f09-5d1b-4f48-a304-5e99e8da1e26',
    status_cron   = '0 6 2 * *'
WHERE id = 'irve'
  AND ocpi_base_url = 'https://www.data.gouv.fr/api/1/datasets/r/7eee8f09-5d1b-4f48-a304-5e99e8da1e26|https://proxy.transport.data.gouv.fr/resource/consolidation-nationale-irve-dynamique';

DELETE FROM cpo WHERE id IN ('es-dgt', 'pt-mobie', 'fi-fintraffic', 'ch-sfoe', 'pl-eipa', 'at-econtrol');
-- +goose StatementEnd
