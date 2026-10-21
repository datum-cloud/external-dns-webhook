FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o datum-dns-webhook .

FROM scratch

COPY --from=builder /app/datum-dns-webhook /

USER 65532:65532

ENTRYPOINT ["/datum-dns-webhook"]
