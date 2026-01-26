# Build stage
FROM golang:1.24-alpine AS builder

# Install CA certificates for HTTPS
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o rediscan .

# Runtime stage - using scratch for minimal image size
FROM scratch

# Copy the binary from builder
COPY --from=builder /app/rediscan /rediscan

# Copy CA certificates for HTTPS (if needed)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Expose port
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/rediscan"]
