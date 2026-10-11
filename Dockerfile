# NE-PULSE backend image. Pure Go (SQLite via modernc), no cgo.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nepulse-server ./cmd/nepulse-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nepulse-server /nepulse-server
# The SQLite database (and optional evidence log) live on a persistent volume.
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/nepulse-server", "-addr", ":8080", "-db", "/data/nepulse.db"]
