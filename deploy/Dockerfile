# syntax=docker/dockerfile:1
# Imagen de produccion de argos: server Go estatico + dashboard + binarios del agente (y .deb) para /dl.
# Contexto = raiz del repo (lo hace docker-compose.yml). Version: --build-arg ARGOS_VERSION=0.1.0
# (debe empezar por digito pa que se genere el .deb).

# --- a) dashboard -----------------------------------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

# --- b) server --------------------------------------------------------------------------------------------
FROM golang:1.27-alpine AS server
ARG ARGOS_VERSION=dev
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${ARGOS_VERSION}" -o /out/argos-server ./cmd/server

# --- c) agentes (6 objetivos + .deb de linux) -------------------------------------------------------------
FROM golang:1.27-alpine AS agents
ARG ARGOS_VERSION=dev
# nfpm fijado pa builds reproducibles (subirlo a mano).
ARG NFPM_VERSION=v2.41.1
RUN apk add --no-cache bash coreutils
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOBIN=/usr/local/bin go install github.com/goreleaser/nfpm/v2/cmd/nfpm@${NFPM_VERSION}
WORKDIR /src
COPY agent/ agent/
COPY scripts/build-agent.sh scripts/
COPY deploy/nfpm/ deploy/nfpm/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    bash scripts/build-agent.sh "${ARGOS_VERSION}" /out/agent

# --- d) imagen final --------------------------------------------------------------------------------------
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 argos && adduser -S -u 10001 -G argos -H -s /sbin/nologin argos
COPY --from=server /out/argos-server /app/argos-server
COPY --from=web /src/web/dist /app/web
COPY --from=agents /out/agent /app/agent
ENV ARGOS_WEB_DIR=/app/web \
    ARGOS_AGENT_DIST_DIR=/app/agent \
    ARGOS_LISTEN_ADDR=0.0.0.0:8080
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/app/argos-server"]
