# syntax=docker/dockerfile:1

# --- Frontend: build the Svelte SPA -----------------------------------------
FROM oven/bun:1.4-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ .
# vite writes the bundle to ../internal/webui/dist, which Go embeds below.
RUN bun run build

# --- Backend: build the self-contained Go binary ----------------------------
FROM golang:1.27-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o bin/downloadarr-lite ./cmd/downloadarr-lite

# --- Runtime ----------------------------------------------------------------
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S downloadarr \
    && adduser -S -G downloadarr downloadarr \
    && mkdir -p /app \
    && chown -R downloadarr:downloadarr /app
COPY --from=backend /app/bin/downloadarr-lite /usr/local/bin/downloadarr-lite
USER downloadarr
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/downloadarr-lite"]
