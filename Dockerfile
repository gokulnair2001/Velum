# ── Build stage ──────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /bin/velum ./cmd/velum

# ── Runtime stage ───────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Download AWS RDS CA bundle so SSL connections to RDS are trusted
RUN wget -qO /usr/local/share/ca-certificates/aws-rds-global-bundle.crt \
    https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem && \
    update-ca-certificates

# Non-root user for security
RUN addgroup -S velum && adduser -S velum -G velum

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/velum .

# Copy default config (secrets come from env vars / .env)
COPY config.yaml .

# Own everything by velum user
RUN chown -R velum:velum /app
USER velum

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./velum"]
