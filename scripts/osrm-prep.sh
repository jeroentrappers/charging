#!/usr/bin/env bash
# One-time (and on-update) OSRM data prep for corridor search.
# Downloads the Belgium OSM extract into ./osrm-data (bind-mounted into the osrm
# service) and runs the MLD pipeline: extract → partition → customize.
#
# Usage (from the repo root, on the deployment host):
#   COMPOSE="docker compose -f docker-compose.prod.yml -f docker-compose.appmire.yml -f docker-compose.osrm.yml" \
#     ./scripts/osrm-prep.sh
#
# After it finishes, start the service:  $COMPOSE up -d osrm
set -euo pipefail

COMPOSE="${COMPOSE:-docker compose -f docker-compose.prod.yml -f docker-compose.osrm.yml}"
PBF_URL="${PBF_URL:-https://download.geofabrik.de/europe/belgium-latest.osm.pbf}"
DATA_DIR="${DATA_DIR:-./osrm-data}"

mkdir -p "$DATA_DIR"

echo "==> Download ${PBF_URL}"
# Download via a throwaway alpine container (busybox wget speaks https) so we
# don't depend on host tools.
docker run --rm -v "$(cd "$DATA_DIR" && pwd):/data" alpine:3 \
  wget -O /data/belgium-latest.osm.pbf "$PBF_URL"

echo "==> extract"
$COMPOSE run --rm --no-deps osrm osrm-extract -p /opt/car.lua /data/belgium-latest.osm.pbf
echo "==> partition"
$COMPOSE run --rm --no-deps osrm osrm-partition /data/belgium-latest.osrm
echo "==> customize"
$COMPOSE run --rm --no-deps osrm osrm-customize /data/belgium-latest.osrm

echo "==> OSRM data ready:"
ls -lh "$DATA_DIR"/belgium-latest.osrm* 2>/dev/null | head
echo "==> Start it with:  ${COMPOSE} up -d osrm"
