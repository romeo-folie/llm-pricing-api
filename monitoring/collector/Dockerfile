# Collector image for the LLM Pricing Platform.
#
# Railway builds each service from the repository and cannot bind-mount a file
# into a container, so the collector config is baked into the image instead of
# mounted (as it is locally by docker-compose.yml).
#
# Reference this file with the service variable
#   RAILWAY_DOCKERFILE_PATH=monitoring/collector/Dockerfile
# so that the API and worker services keep building with Nixpacks via their
# existing start.sh. A Dockerfile at the repository root would be picked up by
# every service and break both of them.
#
# The version is pinned to match docker-compose.yml so local and production run
# the same collector.
FROM otel/opentelemetry-collector-contrib:0.161.0

COPY monitoring/otel-collector.yaml /etc/otelcol-contrib/config.yaml

# Ports the collector expects to have reachable:
#   4317  OTLP gRPC   — the API exports traces here over the private network
#   4318  OTLP HTTP
#   13133 health_check extension — aim the Railway health check at "/" here
# The image's own CMD already points at the config path copied above.
