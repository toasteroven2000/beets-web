# beets-ui

A web UI for browsing a beets Library and Modifying it, through [beets-api](../beets-api). It is modelled on beets-flask's library browser, with Modify added. It's for one person on one machine, so there's no auth and it listens on 127.0.0.1.

Domain language is in [CONTEXT.md](CONTEXT.md) (shared with beets-api), and decisions are in [docs/adr](docs/adr). The v1 spec is [.scratch/ui-v1/spec.md](.scratch/ui-v1/spec.md).

## Running

The toolchain (Go 1.26) comes from [devenv](https://devenv.sh); `direnv allow` loads it on `cd`. Start beets-api first (`devenv up` in `../beets-api`), then either:

```sh
devenv up        # runs the `web` process
serve            # or in the devenv shell: generate templates and run
serve-watch      # regenerate and restart on .templ/.go changes
devenv test      # generate, vet and test
```

Without devenv: `go generate ./... && go run . -api http://127.0.0.1:8338`.

| Flag      | Default                 | Meaning                                         |
| --------- | ----------------------- | ----------------------------------------------- |
| `-listen` | `127.0.0.1:8339`        | Listen address. There's no auth, so keep it local. |
| `-api`    | `http://127.0.0.1:8338` | beets-api base URL                              |

## Layout

| Path            | Contains                                                               |
| --------------- | ---------------------------------------------------------------------- |
| `main.go`       | Flags                                                                  |
| `server.go`     | Routes and handlers                                                    |
| `beetsapi.go`   | The beets-api client                                                   |
| `query.go`      | Building beets queries: Artist regexes, filters, sorting               |
| `pages.go`      | The data each page renders, and template helpers                       |
| `templates.go`  | Rendering and the embedded static assets                               |
| `*.templ`       | HTML views, as [templ](https://templ.guide) components                 |
| `*_templ.go`    | Generated from the `.templ` files; not committed                       |
| `static/*`      | Bootstrap 5.3.8 and htmx 2.0.4 (vendored, unmodified), theme switcher, stylesheet |

The `*_templ.go` files are not committed. Run `go generate ./...` before `go build` or `go test`; CI does the same. Static assets are compiled into the binary.

## Waiting on beets-api

These features need endpoints that beets-api doesn't have yet. Until they exist, their pages say so instead of failing.

- Artist list: `GET /artists` (beets-api `.scratch/artists/`)
- History: `GET /modifications` (beets-api `.scratch/modifications-history/`)
- Album art: `GET /albums/{id}/art` (beets-api `.scratch/album-art/`)
