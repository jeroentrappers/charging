#!/usr/bin/env bash
# OSRM data prep for corridor search (GET /chargers/along-route).
# Downloads one or more OSM extracts, merges them into a single graph, and runs
# the MLD pipeline (extract → partition → customize) into ./osrm-data. Default is
# cross-country (BE+NL+LU+DE+FR) so routes work across borders — matching where
# we have charger data.
#
# Config via env:
#   OSRM_GRAPH    output graph base name        (default: eu-west)
#   OSRM_REGIONS  space-separated Geofabrik paths under download.geofabrik.de/
#                 (default: europe/belgium europe/netherlands europe/luxembourg
#                            europe/germany europe/france)
#   COMPOSE       docker compose invocation (must include the osrm overlay)
#   DATA_DIR      bind-mounted data dir         (default: ./osrm-data)
#
# Requires `osmium` (osmium-tool) on the host when merging >1 region.
# Run from the repo root on the deployment host, then `up -d osrm`.
set -euo pipefail

COMPOSE="${COMPOSE:-docker compose -f docker-compose.prod.yml -f docker-compose.osrm.yml}"
GRAPH="${OSRM_GRAPH:-eu-west}"
REGIONS="${OSRM_REGIONS:-europe/belgium europe/netherlands europe/luxembourg europe/germany europe/france}"
DATA_DIR="${DATA_DIR:-./osrm-data}"
mkdir -p "$DATA_DIR"

pbfs=()
for r in $REGIONS; do
	base="$(basename "$r")-latest.osm.pbf"
	echo "==> download ${r}"
	# Fresh download (no -C/resume: resuming a stale/complete file corrupts the
	# PBF; the box has the bandwidth). -o truncates any prior file.
	curl -fSL --retry 3 --retry-delay 2 -o "$DATA_DIR/$base" "https://download.geofabrik.de/${r}-latest.osm.pbf"
	pbfs+=("$DATA_DIR/$base")
done

merged="$DATA_DIR/${GRAPH}.osm.pbf"
if [ "${#pbfs[@]}" -eq 1 ]; then
	cp -f "${pbfs[0]}" "$merged"
else
	echo "==> merge ${#pbfs[@]} extracts -> ${GRAPH}.osm.pbf"
	osmium merge "${pbfs[@]}" --overwrite -o "$merged"
fi

echo "==> extract"
$COMPOSE run --rm --no-deps osrm osrm-extract -p /opt/car.lua "/data/${GRAPH}.osm.pbf"
echo "==> partition"
$COMPOSE run --rm --no-deps osrm osrm-partition "/data/${GRAPH}.osrm"
echo "==> customize"
$COMPOSE run --rm --no-deps osrm osrm-customize "/data/${GRAPH}.osrm"

echo "==> OSRM data ready:"
ls -lh "$DATA_DIR/${GRAPH}.osrm"* 2>/dev/null | head
echo "==> Start it with:  ${COMPOSE} up -d osrm"
