#!/bin/sh
# Generate /config.js from the VITE_API_BASE env var at container startup.
# nginx:alpine runs every /docker-entrypoint.d/*.sh before starting nginx.
set -eu

: "${VITE_API_BASE:=http://localhost:8080}"
export VITE_API_BASE

# Only substitute VITE_API_BASE (leave any other $tokens untouched).
envsubst '${VITE_API_BASE}' \
  < /etc/charging/config.template.js \
  > /usr/share/nginx/html/config.js

echo "charging: config.js apiBase=${VITE_API_BASE}"
