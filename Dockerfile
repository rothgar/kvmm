# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./
COPY static/ ./static/

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kvmm .

# Runtime stage
FROM scratch

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /build/kvmm /kvmm

EXPOSE 8080

ENTRYPOINT ["/kvmm"]
