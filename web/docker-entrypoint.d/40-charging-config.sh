#!/bin/sh
# Generate /config.js from the VITE_API_BASE env var at container startup.
# nginx:alpine runs every /docker-entrypoint.d/*.sh before starting nginx.
set -eu

: "${VITE_API_BASE:=http://localhost:8080}"
: "${VITE_TILES_URL:=}"
: "${VITE_TILES_KEY:=}"
export VITE_API_BASE VITE_TILES_URL VITE_TILES_KEY

# Only substitute our known tokens (leave any other $tokens untouched).
envsubst '${VITE_API_BASE} ${VITE_TILES_URL} ${VITE_TILES_KEY}' \
  < /etc/charging/config.template.js \
  > /usr/share/nginx/html/config.js

echo "charging: config.js apiBase=${VITE_API_BASE} tilesUrl=${VITE_TILES_URL} tilesKey=${VITE_TILES_KEY:+set}"
