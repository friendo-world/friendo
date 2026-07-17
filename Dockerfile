# friendo network mode — one Go binary hosting many sites by subdomain.
#
# Node-free: the admin SPA (runtime/go/admin/spa) and friendo.js
# (runtime/go/sdk/friendo.js) are committed and embedded via go:embed, so a plain
# `go build` produces a complete binary. Coolify builds this on deploy.

# --- build stage ---
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary (modernc.org/sqlite is pure Go — no CGO needed).
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/friendo ./cli/cmd/friendo

# --- runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /out/friendo /usr/local/bin/friendo

# Config comes from env (see cli/cmd/friendo/network.go + runtime/go/storage):
#   FRIENDO_BASE_DOMAIN     the network's base domain (e.g. friendo.world)
#   FRIENDO_NETWORK_ROOT    where tenant folders + SQLite DBs live (a volume)
#   FRIENDO_PORT            listen port
#   FRIENDO_S3_*            R2/S3 media backend (endpoint/bucket/keys/region)
#   FRIENDO_OPERATOR_PASSWORD, RESEND_API_KEY, FRIENDO_EMAIL_FROM   (optional)
ENV FRIENDO_NETWORK_ROOT=/data/network \
    FRIENDO_PORT=3000

# /data is a Coolify persistent volume: tenant site folders + SQLite databases.
# (Media is offloaded to R2 when FRIENDO_S3_* is set, so this stays small.)
VOLUME ["/data"]
EXPOSE 3000

# NOTE: runs as root so it can write to a freshly-mounted volume without a
# chown dance. Hardening to a non-root user is a follow-up.
CMD ["friendo", "network", "serve"]
