# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o save-sync-go .

# ── Run stage ─────────────────────────────────────────────────────────────────
FROM alpine:latest

WORKDIR /app
COPY --from=builder /build/save-sync-go .

EXPOSE 8080
CMD ["./save-sync-go"]
