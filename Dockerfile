# --- Build stage ---
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies
COPY core/go.mod core/go.sum ./core/
RUN cd core && go mod download

# Copy server source
COPY core/ ./core/

# Build binaries
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN cd core && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/server ./cmd/server
RUN cd core && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o bin/silieco ./cmd/silieco
RUN cd core && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/migrate ./cmd/migrate
RUN cd core && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/backfill_agent_run_usage_hourly ./cmd/backfill_agent_run_usage_hourly
RUN cd core && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/backfill_codex_usage_cache ./cmd/backfill_codex_usage_cache

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /src/core/bin/server .
COPY --from=builder /src/core/bin/silieco .
COPY --from=builder /src/core/bin/migrate .
COPY --from=builder /src/core/bin/backfill_agent_run_usage_hourly .
COPY --from=builder /src/core/bin/backfill_codex_usage_cache .
COPY core/migrations/ ./migrations/
COPY docker/entrypoint.sh .
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
