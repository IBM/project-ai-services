#!/usr/bin/env bash
set -e

if [ -z "$AUTH_CATALOG_CA_BUNDLE" ]; then
  if [ -f /etc/ssl/certs/ca-certificates.crt ]; then
    export AUTH_CATALOG_CA_BUNDLE="/etc/ssl/certs/ca-certificates.crt"
  elif [ -f /etc/pki/tls/certs/ca-bundle.crt ]; then
    export AUTH_CATALOG_CA_BUNDLE="/etc/pki/tls/certs/ca-bundle.crt"
  else
    export AUTH_CATALOG_CA_BUNDLE="/etc/ssl/certs/ca-certificates.crt"
  fi
fi

export CATALOG_API_URL="${CATALOG_API_URL:-http://localhost:8080}"

envsubst '$BACKEND_HOST $BACKEND_PORT $CATALOG_API_URL $AUTH_CATALOG_CA_BUNDLE' \
  < /etc/nginx.conf.tmpl \
  > /etc/nginx/nginx.conf

exec nginx -g "daemon off;"
