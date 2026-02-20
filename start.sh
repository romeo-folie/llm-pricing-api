#!/bin/sh
# Dispatcher for Railway deployments.
# Both the API and worker services are built from the same repo and share
# this start command. Set SERVICE_TYPE=worker on the worker service and
# SERVICE_TYPE=api (or leave unset) on the API service.
set -e

case "${SERVICE_TYPE:-api}" in
  worker)
    exec ./bin/worker
    ;;
  api|*)
    # Apply any pending database migrations before starting the API.
    # Uses the internal DATABASE_URL which is only reachable from within
    # Railway's private network.
    if [ -n "$DATABASE_URL" ] && [ -x ./bin/migrate ]; then
      echo "[migrate] applying pending migrations..."
      ./bin/migrate -path migrations -database "$DATABASE_URL" up
      echo "[migrate] done"
    else
      echo "[migrate] skipped (DATABASE_URL unset or migrate binary missing)"
    fi
    exec ./bin/api
    ;;
esac
