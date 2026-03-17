# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay ./cmd/relay

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /relay /usr/local/bin/agentanycast-relay

EXPOSE 4001/tcp
EXPOSE 4001/udp

ENTRYPOINT ["/usr/local/bin/agentanycast-relay"]
CMD ["--listen", "/ip4/0.0.0.0/tcp/4001"]
