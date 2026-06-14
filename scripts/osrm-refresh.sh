#!/usr/bin/env bash
# Refresh the OSRM routing graph with ~zero downtime. Run monthly by
# osrm-refresh.timer. Builds "<GRAPH>-next" aside (osrm keeps serving the
# current graph during the ~20 min build), then renames it over <GRAPH> and
# restarts osrm so it loads the fresh road network (seconds of downtime).
#
# Env (set by the systemd unit): OSRM_GRAPH, OSRM_REGIONS, COMPOSE, DATA_DIR.
set -euo pipefail
cd "$(dirname "$0")/.."   # repo root (e.g. /opt/charging)

GRAPH="${OSRM_GRAPH:-eu-west}"
COMPOSE="${COMPOSE:-docker compose -f docker-compose.prod.yml -f docker-compose.osrm.yml}"
DATA_DIR="${DATA_DIR:-./osrm-data}"
NEXT="${GRAPH}-next"

echo "==> building ${NEXT} aside"
OSRM_GRAPH="$NEXT" DATA_DIR="$DATA_DIR" COMPOSE="$COMPOSE" ./scripts/osrm-prep.sh

# Don't swap unless the new graph actually built.
if [ ! -s "${DATA_DIR}/${NEXT}.osrm.cell_metrics" ]; then
	echo "!! ${NEXT}.osrm.cell_metrics missing — aborting, leaving the live graph untouched" >&2
	exit 1
fi

echo "==> swapping ${NEXT} -> ${GRAPH}"
# osrm keeps the old (now-unlinked) inodes mapped until it restarts, so renaming
# over the live files is safe; the restart picks up the new ones.
for f in "${DATA_DIR}/${NEXT}.osrm"*; do
	base="$(basename "$f")"
	mv -f "$f" "${DATA_DIR}/${GRAPH}${base#${NEXT}}"
done

# The .osm.pbf sources aren't needed at runtime; the next refresh re-downloads.
rm -f "${DATA_DIR}"/*.osm.pbf

echo "==> restarting osrm"
$COMPOSE restart osrm
echo "==> osrm graph ${GRAPH} refreshed"
