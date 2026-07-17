# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /xyncra-server ./cmd/xyncra-server/

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates curl && \
    adduser -D -u 1000 xyncra

WORKDIR /app
COPY --from=builder /xyncra-server .
COPY --from=builder /build/agents ./agents

USER xyncra

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["./xyncra-server"]
