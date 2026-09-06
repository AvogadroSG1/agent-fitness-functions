# syntax=docker/dockerfile:1

FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS go-build
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY patterns/ patterns/
RUN : "${TARGETOS:?TARGETOS is required}" \
    && : "${TARGETARCH:?TARGETARCH is required}" \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-w -s" \
    -o /out/agent-fitness-functions ./cmd/agent-fitness-functions

FROM mcr.microsoft.com/dotnet/sdk:8.0.301 AS dotnet-build
WORKDIR /src/tools/roslyn-analyzer
ARG TARGETARCH

COPY tools/roslyn-analyzer/CalmRoslynAnalyzer.csproj ./
RUN dotnet restore

COPY tools/roslyn-analyzer/ ./
RUN : "${TARGETARCH:?TARGETARCH is required}" \
    && case "$TARGETARCH" in \
        amd64) rid=linux-x64 ;; \
        arm64) rid=linux-arm64 ;; \
        *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && dotnet publish -c Release --self-contained true -r "$rid" -o /out/roslyn

# CALM's dependency engines require Node >=20. Match the final Debian bookworm
# runtime while pinning the official Node multi-platform image.
FROM node:24-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e AS node-runtime

FROM mcr.microsoft.com/dotnet/runtime-deps:8.0.6

ARG GIT_SHA=dev
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.revision=$GIT_SHA \
      org.opencontainers.image.created=$BUILD_DATE

WORKDIR /app

RUN groupadd -g 1001 appuser \
    && useradd -u 1001 -g appuser -s /usr/sbin/nologin -M appuser

COPY requirements.lock /tmp/requirements.lock
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node-runtime /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN apt-get update \
    && apt-get install --no-install-recommends -y \
        ca-certificates \
        curl \
        python3 \
        python3-pip \
    && python3 -m pip install --no-cache-dir --break-system-packages --require-hashes -r /tmp/requirements.lock \
    && ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && npm_config_engine_strict=true npm install -g @finos/calm-cli@1.40.0 \
    && npm cache clean --force \
    && rm -f /tmp/requirements.lock \
    && find /var/lib/apt/lists -mindepth 1 -delete

COPY --from=go-build --chown=appuser:appuser /out/agent-fitness-functions /app/agent-fitness-functions
COPY --from=dotnet-build --chown=appuser:appuser /out/roslyn/ /app/tools/roslyn-analyzer/bin/Release/net8.0/

VOLUME ["/app/configs"]
EXPOSE 7890

# Managed Compose uses its pinned runtime CA. External TLS uses its configured CA,
# while direct plain-HTTP images remain health-checkable without runtime artifacts.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD if test -f /run/agent-fitness-functions/health-ca.crt; then curl --fail --silent --cacert /run/agent-fitness-functions/health-ca.crt https://127.0.0.1:7890/health; elif [ -n "$AGENT_FITNESS_FUNCTIONS_TLS_CA" ]; then curl --fail --silent --cacert "$AGENT_FITNESS_FUNCTIONS_TLS_CA" https://127.0.0.1:7890/health; else curl --fail --silent http://127.0.0.1:7890/health; fi || exit 1

USER appuser
ENTRYPOINT ["/app/agent-fitness-functions", "server", "start"]
CMD ["--addr", "0.0.0.0:7890"]
