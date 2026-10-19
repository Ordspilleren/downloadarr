# downloadarr

A deliberately small torrent importer: paste a magnet link or upload
a `.torrent` file (or drag one onto the page) in the web UI, a torrent client
downloads it, and the mover hard-links the finished files into your movie or TV
library. There is no indexer, no database and no ranking. TMDB integration is
optional: it turns the release name into canonical metadata that the user can
correct before the import.

## How it works

```
browser ──POST /api/downloads──▶ queue ──▶ downloader (qBittorrent)
   ▲                               │  poll progress every poll_interval
   │   SSE GET /api/events         │
   │                               ├─▶ TMDB search (async auto-match)
   │                               │
   │   PATCH …/match ──────────────▶  user correction (movie/series, S/E)
   └──── full download list ───────┤
                                   ▼  torrent complete (after match settles)
                     mediamover hard-links into library
```

Progress, match and status changes are pushed to the browser over Server-Sent
Events, so the UI is live without polling. Only the backend polls the torrent
client.

The media metadata starts as a guess parsed from the torrent's display name:
`release.Parse` recognises `S01E03` / `1x03` episodes, `S01` season packs, and
`Title.Year` movies. When a TMDB API key is configured, the queue then
searches TMDB and replaces the guess with a canonical title (and, for TV,
fetches episode titles from the matched season). The user can correct the match
from the download card at any time before the import. Downloads live in memory
only, so the list is empty after a restart (any torrent still in the client
keeps downloading; re-add its magnet to import it). Each download offers two
actions: **Clear** forgets the entry in memory but leaves the torrent alone in
the client (so it keeps downloading or seeding), while **Delete** removes the
torrent from the client and asks whether its files should be deleted too.

## Layout

| Package | Responsibility |
|---|---|
| `cmd/downloadarr` | composition root: config → wiring → start/stop |
| `config` | YAML loading, defaults and validation |
| `downloader` | qBittorrent client, a dev `fake` client, magnet/torrent info-hash parsing |
| `mediamover` | hard-links (or copies) finished content; template paths |
| `metadata` | minimal TMDB client: search, lookup, season episodes |
| `release` | parses a release name into movie/episode/season metadata |
| `queue` | tracks downloads, polls the client, triggers imports |
| `server` | JSON API + serves the embedded Svelte SPA |
| `internal/webui` | `go:embed` of the frontend build (generated, not committed) |
| `web` | Svelte 5 + TypeScript (no SvelteKit) frontend, built by Vite |

## Build & run

The frontend build output (`internal/webui/dist`) is generated, not committed.
Build it first (requires Bun), then build the Go binary:

```sh
make build          # cd web && bun install && bun run check && bun run build, then go build
./bin/downloadarr -config config.yaml
```

`make web` builds just the frontend. It must be built before the Go build:
`go build` fails with `pattern all:dist: no matching files found` until
`internal/webui/dist` exists.

Open http://localhost:8080 and paste a magnet link, pick a `.torrent` file, or
drag one onto the page.

### Trying it without qBittorrent

Set `downloader.type: fake` in `config.yaml`. A fake torrent completes after a
few seconds and materialises episode- or movie-shaped files under
`$TMPDIR/downloadarr-fake/`, so the whole flow can be exercised offline.

### Frontend development

```sh
# terminal 1: Go backend on :8080
go run ./cmd/downloadarr
# terminal 2: Vite dev server on :5173 (proxies /api to :8080)
cd web && bun run dev
```

`svelte-check` type-checks the frontend (`make check`, or `bun run check` from
`web/`). It runs as part of `make build`; `bun run check:watch` watches for
changes during development.

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/downloads` | current list, newest first |
| `POST` | `/api/downloads` | add a magnet (`{"magnet": "magnet:?xt=…"}`) or a `.torrent` file (`multipart/form-data` with a `file` field) |
| `DELETE` | `/api/downloads/:hash` | clear a download from the list, leaving the torrent in the client |
| `DELETE` | `/api/downloads/:hash?deleteFromClient=true&deleteFiles=true` | remove a download from the client and forget it; `deleteFiles` (default false) also deletes the data |
| `PATCH` | `/api/downloads/:hash/match` | correct the metadata: `{"mediaType":"movie","tmdbId":603,"season":0,"episode":0}` (or `title`/`year` for a manual override) |
| `GET` | `/api/search?type=movie&query=…` | TMDB search for the match editor |
| `GET` | `/api/status` | `{"metadataEnabled": true|false}` |
| `GET` | `/api/trackers` | `[{"name":"…","url":"…"}]` tracker search endpoints, `[]` when unset |
| `GET` | `/api/events` | SSE stream: the full list on every change, plus keep-alive comments |

## Configuration

```yaml
general:
  listen_addr: ":8080"

downloader:
  type: qbittorrent       # or "fake"
  url: "http://localhost:8080"
  username: admin
  password: adminadmin
  category: downloadarr
  poll_interval: 2s

metadata:
  tmdb:
    # Optional. Without a key the app moves content using the release name
    # alone. Accepts a v3 API key or a v4 bearer token.
    api_key: ""

trackers:
  # Optional. While the add field contains keywords instead of a magnet link,
  # the UI links into each tracker below. "{query}" is replaced with the
  # URL-encoded keywords.
  - name: Example Tracker
    url: "https://tracker.example/search?q={query}"
  - name: Another Tracker
    url: "https://another.example/browse.php?search={query}&order=seeders"

media_paths:
  movies:
    path: /media/movies
    template: '{{.MovieName}}{{if .Year}} ({{.Year}}){{end}}/{{.MovieName}}{{if .Year}} ({{.Year}}){{end}}'
  tv:
    path: /media/tv
    template: '{{.ShowName}}/Season {{.SeasonNumber | padZeros}}/{{.ShowName}} - S{{.SeasonNumber | padZeros}}E{{.EpisodeNumber | padZeros}}{{if .EpisodeTitle}} - {{.EpisodeTitle}}{{end}}'
```

### Tracker search

The add field is dual-purpose: a value starting with `magnet:` downloads as
before, while any other non-empty value is treated as keywords and shows a
live list of tracker search links below the form. Each tracker comes from the
top-level `trackers` list above, and every `{query}` in its `url` is replaced
with the URL-encoded keywords. The Download button is disabled outside magnet
mode. Trackers are optional; with none configured, query mode shows a hint.

Path templates are Go templates. Available fields are:

- movies: `{{.MovieName}}`, `{{.Year}}`
- TV: `{{.ShowName}}`, `{{.SeasonNumber}}`, `{{.EpisodeNumber}}`, `{{.EpisodeTitle}}`

Helpers: `padZeros` (renders `1` as `01`) and `sanitize` (strips characters
that are invalid in a path). If a template does not include a file extension,
the source file's extension is kept.

## Metadata matching

With `metadata.tmdb.api_key` set, every added magnet gets an asynchronous TMDB
lookup:

- The release name is parsed for a title, year, season and episode, then TMDB
  is searched for the guessed type. The best hit (exact year preferred) becomes
  the download's metadata; the alternatives are kept as candidates.
- The download card shows the matched title, year, type and a status badge
  (`auto-matched`, `corrected`, or `release name` when no match was found).
- **Fix match** opens an editor where the type can be switched, TMDB searched,
  a candidate chosen, and season/episode adjusted. A manual title/year override
  is available for content TMDB does not have.
- The import waits briefly for a pending lookup, so a fast download is not
  moved before the match settles.
- For TV, episode titles are fetched from the matched season, so season packs
  import as `… S01E03 - Episode Title.mkv` rather than bare episode numbers.

Without an API key, the app silently falls back to the release name and
behaves exactly as before.

## Import rules

- **Movies / single episodes**: the largest video file is imported; samples and
  extras are skipped.
- **Season packs**: every video file is imported to its own per-episode
  destination; files without an episode number or from another season are
  skipped.
- **Episode grab that is actually a pack**: if an `SxxEyy` download contains
  multiple video files, the whole pack is imported instead of dropping episodes.
- Files are hard-linked so the torrent keeps seeding; across filesystems the
  mover falls back to a copy.
