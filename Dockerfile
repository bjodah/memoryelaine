# Build stage
FROM golang:1.25-bookworm AS builder
RUN apt-get update && apt-get install -y gcc libc6-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w \
    -X memoryelaine/internal/version.Version=$(git describe --tags --always 2>/dev/null || echo dev) \
    -X memoryelaine/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /memoryelaine .

# Runtime stage
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /memoryelaine /usr/local/bin/memoryelaine
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 8000 8080
ENTRYPOINT ["memoryelaine"]
CMD ["serve", "--config", "/data/config.yaml"]
