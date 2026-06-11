#!/usr/bin/env bash
# One-time (and on-update) OSRM data prep for corridor search.
# Downloads the Belgium OSM extract and runs the MLD pipeline
# (extract → partition → customize) into the shared osrm-data volume, using a
# throwaway container on the same compose project so the volume name matches.
#
# Usage (from the repo root, on the deployment host):
#   COMPOSE="docker compose -f docker-compose.prod.yml -f docker-compose.appmire.yml -f docker-compose.osrm.yml" \
#     ./scripts/osrm-prep.sh
#
# After it finishes, start the service:  $COMPOSE up -d osrm
set -euo pipefail

COMPOSE="${COMPOSE:-docker compose -f docker-compose.prod.yml -f docker-compose.osrm.yml}"
PBF_URL="${PBF_URL:-https://download.geofabrik.de/europe/belgium-latest.osm.pbf}"

echo "==> Preparing OSRM data from ${PBF_URL}"
# A one-off container with the osrm image + the osrm-data volume. The default
# car profile ships at /opt/car.lua inside the image.
$COMPOSE run --rm --no-deps --entrypoint bash osrm -c "
  set -euo pipefail
  cd /data
  echo '--> download'
  curl -fSL -o belgium-latest.osm.pbf '${PBF_URL}'
  echo '--> extract'
  osrm-extract -p /opt/car.lua belgium-latest.osm.pbf
  echo '--> partition'
  osrm-partition belgium-latest.osrm
  echo '--> customize'
  osrm-customize belgium-latest.osrm
  echo '--> done'; ls -lh /data/belgium-latest.osrm*
"
echo "==> OSRM data ready. Start it with:  ${COMPOSE} up -d osrm"
