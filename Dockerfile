FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o datum-dns-webhook .

# Use distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app/datum-dns-webhook /

USER nonroot:nonroot

ENTRYPOINT ["/datum-dns-webhook"]
