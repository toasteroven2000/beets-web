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
- Audit: `GET /audit`, and `GET /audit/{id}` for one entry
- Album art: `GET /albums/{id}/art` (beets-api `.scratch/album-art/`)

## Album art

The Art tab searches the iTunes Search API and the Deezer API (no keys), and fetches images from URLs you enter, so beets-ui needs outbound HTTPS. Set art sends the image to beets-api's `PUT /albums/{id}/art`, which saves it as `cover.jpg` or `cover.png`; Remove art calls `DELETE /albums/{id}/art`. beets-api moves replaced and removed art to its Trash. Every request carries `X-Beets-API-Client: beets-ui`, so beets-api's audit shows which changes came from the UI. When an Album already has art, the UI shows the current and new art side by side first; the fetched image waits in memory for up to an hour.

## Import

The Import page has two tabs. Import Paths browses beets-api's Import Paths (`GET /import-paths`, set with `BEETS_API_IMPORT_PATHS`) and imports one folder at a time; History lists past Imports, filterable by status. beets-ui never reads the files itself, so it needs no access to them. Whether files are copied or moved is beets' `import` config; the prompt says which beside the best Candidate's Apply button, and warns when the originals will be deleted. An Import's page has three tabs. **Now** says which Task it is on and out of how many beets expects ("Task 3 of 50", a progress bar, how many were applied, used as-is or skipped, and the folder coming up next), then the prompt, or what beets is doing, or how it ended. **Log** lists every Task: the one beets is asking about, those done (newest first, with how each went and who decided), and those still to come, with filters for applied, as-is, skipped and remaining. **Paths** says what was asked for. The total and the list of what is to come come from beets-api (`expected_tasks`, `tasks_done` and `GET /imports/{id}/tasks`); the total is an estimate, since "Import as tracks" and "Group albums" change the Tasks after them, and without it the page still shows the Task number. While beets works the Import page polls every second; when beets needs a Choice it shows the Candidates with their field changes, and the album, item or duplicate choices beets offers. "Remove old" on a duplicate deletes the existing files, so it asks first.

A finished Import has an "Undo this Import…" button (`GET` and `POST /imports/{id}/undo` in beets-api). It previews what would be removed and asks before deleting the copied files. beets-api only allows it for an Import that copied its files, when nothing it made has changed since, so otherwise the page lists the reasons.

beets-api only accepts `Host: 127.0.0.1` or `localhost`, so beets-ui in another container can't reach it yet (beets-api `.scratch/imports/issues/02-allowed-hosts.md`).

## Remove

Every Album and Item list has **Remove selected** and **Remove all N** (the latter only when the list has a query), and each Album and Item page has **Remove…**. They ask beets-api for a Removal Preview (`POST /removals`), which lists what would go and how many files would be deleted, and nothing is removed until you confirm on that page. Confirming removes the records and moves their files to beets-api's Trash, where they stay until you Purge it from the Trash page; files outside the music directory are left where they are. beets-api refuses to remove anything while `BEETS_API_TRASH_DIR` is unset, and the page says so. Removals are listed under the person menu, and each removed Album or Item is in the Audit, linked to its Removal.

## Trash and Purge

beets-api never deletes a file: removals, Import undo, a duplicate replaced by an Import, and replaced or removed art all move files into its **Trash**, one folder per operation. The Trash page (person menu) lists the folders with their size, age, what made them and who, and offers **Purge selected…**, **Purge everything older than…** and **Purge everything…**. Each asks beets-api for a Purge Preview (`POST /purges`) and deletes nothing until you confirm on that page, which says how many files and bytes go. A Purge is the one step that can't be undone. Purges are listed under Purge history, and each folder it deletes is in the Audit as `trash.purge`.

The Trash is off until beets-api has `BEETS_API_TRASH_DIR`. While it is unset beets-api refuses everything that would delete a file, and beets-ui says so on the Removal, Remove old and Purge pages and disables their confirm buttons, instead of leaving a refusal looking like a stale Preview. There is no way to put trashed files back yet.

## Who is making a change

beets-ui has no login of its own. Put it behind a reverse proxy that signs people in, such as Traefik with Authelia, which sends the person's name in headers (`Remote-User`, `Remote-Name`, `Remote-Email`, `Remote-Groups`). beets-ui requires the header named by `-actor-header` (default `Remote-User`) on every request and answers `401` without it, so nothing is written without saying who wrote it. It sends that name to beets-api as `X-Beets-API-Actor`, and beets-api records it on each Audit entry and each Choice of an Import. Authorization isn't done: everyone the proxy admits can do everything, and `Remote-Groups` is only shown.

`GET /ping` answers `200 ok` without the header, for health checks. It doesn't call beets-api.

The headers are only trustworthy when the proxy is the only way to reach beets-ui, since anything that connects directly can send them itself. Don't publish beets-ui's port. Likewise only beets-ui should be able to reach beets-api, which trusts the Actor it is sent.

In development, add the header with a browser extension. The person's menu (their name, top right) holds Profile, which shows the headers beets-ui received and which one is the Actor, then Modifications and the Audit.
