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
| `art.go`        | Album art: searching iTunes and Deezer, fetching an image to Set art   |
| `imports.go`    | Import: browsing Import Paths, starting an Import, answering its prompts |
| `query.go`      | Building beets queries: Artist regexes, filters, sorting               |
| `pages.go`      | The data each page renders, and template helpers                       |
| `templates.go`  | Rendering and the embedded static assets                               |
| `*.templ`       | HTML views, as [templ](https://templ.guide) components                 |
| `*_templ.go`    | Generated from the `.templ` files; not committed                       |
| `static/*`      | Bootstrap 5.3.8, Bootstrap Icons 1.13.1 (CSS and woff2 font), the MusicBrainz logo from [dashboard-icons](https://dashboardicons.com/icons/musicbrainz) (Apache-2.0) and htmx 2.0.4 (vendored, unmodified), theme switcher, stylesheet |

The `*_templ.go` files are not committed. Run `go generate ./...` before `go build` or `go test`; CI does the same. Static assets are compiled into the binary.

## Waiting on beets-api

These features need endpoints that beets-api doesn't have yet. Until they exist, their pages say so instead of failing.

- Artist list: `GET /artists` (beets-api `.scratch/artists/`)
- Modifications list: `GET /modifications` (beets-api `.scratch/modifications-history/`)
- Audit: `GET /audit`
- Album art: `GET /albums/{id}/art` (beets-api `.scratch/album-art/`)

## Album art

The Art tab searches the iTunes Search API and the Deezer API (no keys), and fetches images from URLs you enter, so beets-ui needs outbound HTTPS. Set art sends the image to beets-api's `PUT /albums/{id}/art`, which saves it as `cover.jpg` or `cover.png`; Remove art calls `DELETE /albums/{id}/art`. beets-api archives replaced and removed art. Every request carries `X-Client: beets-ui`, so beets-api's audit shows which changes came from the UI. When an Album already has art, the UI shows the current and new art side by side first; the fetched image waits in memory for up to an hour.

## Import

The Import page has two tabs. Import Paths browses beets-api's Import Paths (`GET /import-paths`, set with `BEETS_API_IMPORT_PATHS`) and imports one folder at a time; History lists past Imports, filterable by status. beets-ui never reads the files itself, so it needs no access to them. Whether files are copied or moved is beets' `import` config; the page says which before you start, and warns when the originals will be deleted. While beets works the Import page polls every second; when beets needs a Choice it shows the Candidates with their field changes, and the album, item or duplicate choices beets offers. "Remove old" on a duplicate deletes the existing files, so it asks first.

beets-api only accepts `Host: 127.0.0.1` or `localhost`, so beets-ui in another container can't reach it yet (beets-api `.scratch/imports/issues/02-allowed-hosts.md`).
