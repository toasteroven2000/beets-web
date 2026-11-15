# Minimal image: a static Go binary on distroless, running as non-root.
# Pinned to the Go version go.mod asks for.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The *_templ.go files are not committed.
RUN go tool templ generate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /beets-web .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /beets-web /beets-web
# 0.0.0.0 so the container is reachable; keep it behind the proxy that signs people in.
ENV BEETS_WEB_LISTEN=:8339
EXPOSE 8339
ENTRYPOINT ["/beets-web"]
