FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto

RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/sync-server ./cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install --no-install-recommends --yes ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-create-home syncapp

COPY --from=build /out/sync-server /usr/local/bin/sync-server

USER syncapp
EXPOSE 54321/tcp 8080/tcp

ENV SYNC_LISTEN=:54321 \
    HTTP_LISTEN=:8080

ENTRYPOINT ["/usr/local/bin/sync-server"]
