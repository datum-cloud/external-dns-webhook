FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o datum-dns-webhook .

RUN echo 'nonroot:x:65532:65532:nonroot:/home/nonroot:/sbin/nologin' > /app/passwd && \
    echo 'nonroot:x:65532:' > /app/group

FROM scratch

COPY --from=builder /app/passwd /etc/passwd
COPY --from=builder /app/group /etc/group
COPY --from=builder /app/datum-dns-webhook /

USER 65532:65532

ENTRYPOINT ["/datum-dns-webhook"]
