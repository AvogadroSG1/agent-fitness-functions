# syntax=docker/dockerfile:1

FROM golang:1.22.4-alpine3.20 AS go-build
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
    -o /out/calm-bridge ./cmd/calm-bridge

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

FROM mcr.microsoft.com/dotnet/runtime-deps:8.0.6

ARG GIT_SHA=dev
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.revision=$GIT_SHA \
      org.opencontainers.image.created=$BUILD_DATE

WORKDIR /app

RUN groupadd -g 1001 appuser \
    && useradd -u 1001 -g appuser -s /usr/sbin/nologin -M appuser

COPY requirements.lock /tmp/requirements.lock
RUN apt-get update \
    && apt-get install --no-install-recommends -y \
        ca-certificates \
        curl \
        nodejs \
        npm \
        python3 \
        python3-pip \
    && python3 -m pip install --no-cache-dir --break-system-packages --require-hashes -r /tmp/requirements.lock \
    && npm install -g @finos/calm-cli@1.40.0 \
    && npm cache clean --force \
    && rm -f /tmp/requirements.lock \
    && find /var/lib/apt/lists -mindepth 1 -delete

COPY --from=go-build --chown=appuser:appuser /out/calm-bridge /app/calm-bridge
COPY --from=dotnet-build --chown=appuser:appuser /out/roslyn/ /app/tools/roslyn-analyzer/bin/Release/net8.0/

VOLUME ["/app/configs"]
EXPOSE 7890

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD if [ -n "$CALM_TLS_CA" ]; then curl --fail --silent --cacert "$CALM_TLS_CA" https://127.0.0.1:7890/health; else curl --fail --silent http://127.0.0.1:7890/health; fi || exit 1

USER appuser
ENTRYPOINT ["/app/calm-bridge", "serve"]
CMD ["--addr", "0.0.0.0:7890"]
