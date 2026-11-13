# beets-ui

A web UI for browsing a beets Library and Modifying it, through [beets-api](../beets-api). It is modelled on beets-flask's library browser, with Modify added. It signs nobody in: a reverse proxy does, and every request must say who it is from (see [Who is making a change](#who-is-making-a-change)). It listens on 127.0.0.1 unless told otherwise.

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
| `-listen` | `127.0.0.1:8339`        | Listen address. Reach it only through the proxy that signs people in. |
| `-api`    | `http://127.0.0.1:8338` | beets-api base URL                              |
| `-actor-header` (`BEETS_UI_ACTOR_HEADER`) | `Remote-User` | The request header that names the person making it, which beets-api records as the Actor. A request without it gets a 401. |

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

The Art tab searches the iTunes Search API and the Deezer API (no keys), and fetches images from URLs you enter, so beets-ui needs outbound HTTPS. Set art sends the image to beets-api's `PUT /albums/{id}/art`, which saves it as `cover.jpg` or `cover.png`; Remove art calls `DELETE /albums/{id}/art`. beets-api archives replaced and removed art. Every request carries `X-Beets-API-Client: beets-ui`, so beets-api's audit shows which changes came from the UI. When an Album already has art, the UI shows the current and new art side by side first; the fetched image waits in memory for up to an hour.

## Import

The Import page has two tabs. Import Paths browses beets-api's Import Paths (`GET /import-paths`, set with `BEETS_API_IMPORT_PATHS`) and imports one folder at a time; History lists past Imports, filterable by status. beets-ui never reads the files itself, so it needs no access to them. Whether files are copied or moved is beets' `import` config; the prompt says which beside the best Candidate's Apply button, and warns when the originals will be deleted. While beets works the Import page polls every second; when beets needs a Choice it shows the Candidates with their field changes, and the album, item or duplicate choices beets offers. "Remove old" on a duplicate deletes the existing files, so it asks first.

A finished Import has an "Undo this Import…" button (`GET` and `POST /imports/{id}/undo` in beets-api). It previews what would be removed and asks before deleting the copied files. beets-api only allows it for an Import that copied its files, when nothing it made has changed since, so otherwise the page lists the reasons.

beets-api only accepts `Host: 127.0.0.1` or `localhost`, so beets-ui in another container can't reach it yet (beets-api `.scratch/imports/issues/02-allowed-hosts.md`).

## Who is making a change

beets-ui has no login of its own. Put it behind a reverse proxy that signs people in, such as Traefik with Authelia, which sends the person's name in headers (`Remote-User`, `Remote-Name`, `Remote-Email`, `Remote-Groups`). beets-ui requires the header named by `-actor-header` (default `Remote-User`) on every request and answers `401` without it, so nothing is written without saying who wrote it. It sends that name to beets-api as `X-Beets-API-Actor`, and beets-api records it on each Audit entry and each Choice of an Import. Authorization isn't done: everyone the proxy admits can do everything, and `Remote-Groups` is only shown.

`GET /ping` answers `200 ok` without the header, for health checks. It doesn't call beets-api.

The headers are only trustworthy when the proxy is the only way to reach beets-ui, since anything that connects directly can send them itself. Don't publish beets-ui's port. Likewise only beets-ui should be able to reach beets-api, which trusts the Actor it is sent.

In development, add the header with a browser extension. The person's menu (their name, top right) holds Profile, which shows the headers beets-ui received and which one is the Actor, then Modifications and the Audit.
