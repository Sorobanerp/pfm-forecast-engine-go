# syntax=docker/dockerfile:1.7
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags="-s -w" \
    -o /out/pfm-forecast-engine ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/pfm-forecast-engine /app/pfm-forecast-engine

USER nonroot:nonroot
EXPOSE 9091
ENTRYPOINT ["/app/pfm-forecast-engine"]
