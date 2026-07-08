#!/usr/bin/env bash
# Validate DATEX II payload XML against the OFFICIAL v3.7 schema set from
# docs.datex2.eu (cached in .datex-xsd/, gitignored). There is no public web
# validator for v3.7 AFIR instance documents — XSD validation against the
# published schemas IS the reference check (see docs/sources.md).
#
# Usage:
#   scripts/validate-datex.sh [file-or-url ...]
#
# URLs are fetched with "Authorization: Bearer $DATEX_TOKEN" when set. With no
# arguments it validates the live EnergyVision table+status pair, using
# DATEX_TOKEN or ENERGYVISION_TOKEN (e.g. from .env via `make validate-datex`).
set -euo pipefail

cd "$(dirname "$0")/.."
XSD_DIR=.datex-xsd/v3.7
XSD_BASE=https://docs.datex2.eu/_static/data/v3.7
ROOT_XSD=$XSD_DIR/DATEXII_3_D2Payload.xsd
# Everything DATEXII_3_D2Payload.xsd imports (full official namespace set).
MODULES=(AfirEnergyInfrastructure AfirFacilities Common CommonExtension
    ControlledZone D2Payload EnergyInfrastructure Facilities FaultAndStatus
    LocationExtension LocationReferencing Parking ReroutingManagementEnhanced
    RoadTrafficData Situation TrafficManagementPlan TrafficRegulation
    UrbanExtensions Vms)

command -v xmllint >/dev/null || { echo "xmllint not found (install libxml2)"; exit 1; }

mkdir -p "$XSD_DIR"
for m in "${MODULES[@]}"; do
    f=$XSD_DIR/DATEXII_3_$m.xsd
    if [ ! -s "$f" ]; then
        echo "fetching official schema DATEXII_3_$m.xsd"
        curl -fsS -o "$f" "$XSD_BASE/DATEXII_3_$m.xsd"
    fi
done

TOKEN=${DATEX_TOKEN:-${ENERGYVISION_TOKEN:-}}
TARGETS=("$@")
if [ ${#TARGETS[@]} -eq 0 ]; then
    [ -n "$TOKEN" ] || { echo "no feeds given and no ENERGYVISION_TOKEN/DATEX_TOKEN set"; exit 1; }
    TARGETS=(
        "https://datex.cpo.energyvision.be/datex/energy-infrastructure-table"
        "https://datex.cpo.energyvision.be/datex/energy-infrastructure-status"
    )
fi

fail=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for t in "${TARGETS[@]}"; do
    case "$t" in
    http://*|https://*)
        f=$tmp/$(basename "${t%%\?*}").xml
        auth=()
        [ -n "$TOKEN" ] && auth=(-H "Authorization: Bearer $TOKEN")
        curl -fsS "${auth[@]}" -H "Accept: application/xml" -o "$f" "$t" ||
            { echo "FAIL  $t (fetch failed)"; fail=1; continue; }
        ;;
    *)
        f=$t
        ;;
    esac
    size=$(wc -c <"$f" | tr -d ' ')
    if out=$(xmllint --noout --schema "$ROOT_XSD" "$f" 2>&1); then
        echo "OK    $t (${size} bytes) validates against official DATEX II v3.7"
    else
        echo "FAIL  $t (${size} bytes)"
        echo "$out" | head -20
        fail=1
    fi
done
exit $fail
