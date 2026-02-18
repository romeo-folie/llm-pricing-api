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
    exec ./bin/api
    ;;
esac
