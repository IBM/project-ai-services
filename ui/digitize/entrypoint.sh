#!/usr/bin/env bash
set -e

envsubst '$BACKEND_HOST $BACKEND_PORT' \
  < /etc/nginx/nginx.conf.tmpl \
  > /etc/nginx/nginx.conf

exec nginx -g "daemon off;"

# Made with Bob
