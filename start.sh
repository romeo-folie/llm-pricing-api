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
    # Note: the migrate CLI uses lib/pq which defaults to sslmode=require.
    # Railway's internal Postgres does not enable SSL, so we must append
    # sslmode=disable. The Go app itself uses pgx which negotiates SSL
    # gracefully (sslmode=prefer), so this only affects the migrate binary.
    if [ -n "$DATABASE_URL" ] && [ -x ./bin/migrate ]; then
      echo "[migrate] applying pending migrations..."
      # Append sslmode=disable, using the correct separator (? or &)
      case "$DATABASE_URL" in
        *\?*) MIGRATE_URL="${DATABASE_URL}&sslmode=disable" ;;
        *)    MIGRATE_URL="${DATABASE_URL}?sslmode=disable" ;;
      esac
      ./bin/migrate -path migrations -database "$MIGRATE_URL" up || {
        echo "[migrate] WARNING: migration failed, starting API anyway"
      }
      echo "[migrate] done"
    else
      echo "[migrate] skipped (DATABASE_URL unset or migrate binary missing)"
    fi
    exec ./bin/api
    ;;
esac
