# Build all three binaries, ship them on a minimal static base.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/ingest ./cmd/ingest \
 && CGO_ENABLED=0 GOFLAGS=-trimpath go build -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /out/ingest /out/migrate /usr/local/bin/
USER nonroot:nonroot
# Default command; docker-compose overrides per service.
CMD ["/usr/local/bin/api"]
