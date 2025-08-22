# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o agentry \
    main.go

# Final stage
FROM scratch

# Import from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/agentry /agentry

# Set environment variables
ENV TZ=UTC
ENV AMTP_SERVER_ADDRESS=:8443
ENV AMTP_DOMAIN=localhost
ENV AMTP_TLS_ENABLED=false
ENV AMTP_LOG_LEVEL=info
ENV AMTP_LOG_FORMAT=json

# Expose port
EXPOSE 8443

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/agentry", "--health-check"] || exit 1

# Run the binary
ENTRYPOINT ["/agentry"]