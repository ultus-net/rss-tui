# rss-tui design

## Intent

Keep a feed reader open in a dedicated terminal or tmux pane without it
becoming another browser tab. It must be cheap to leave running, keyboard-first,
and quiet unless something happened.

Non-goals: a web UI, article archiving, image rendering, or an account system.
The reader reads feeds and tracks what you have seen.

## Decisions

Four choices were settled with the operator before implementation.

| Question | Decision | Why |
| --- | --- | --- |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no CGO. Indexed unread counts and atomic read-state writes stay fast with thousands of cached articles. A JSON file rewrites everything on each mark-read. |
| Notifications | Opt-in desktop notification, coalesced per cycle | Quiet first run. One popup per poll, never one per article. Missing daemon is ignored. |
| Themes | Six IDE palettes, `t` cycles live, choice persisted in `config.yaml` | Palettes are data, not UI code. Live cycling makes a low-glare choice cheap to find. |
| Narrow terminals | Single pane under 100 cols, `Tab` forces it | Small tmux panes are the primary use case. Squeezing two panes there helps nobody. |

Alternatives rejected: a single-file JSON state (write amplification), an
always-on notification stream (noise), a config-only theme switch (slow to
evaluate), and keeping the split at every width (unreadable below ~90 cols).

## Packages and dependency direction

```text
cmd/rss-tui
    |
    +-- internal/config       YAML, defaults, validation, XDG paths, save/merge
    +-- internal/store        SQLite schema and queries
    +-- internal/feed         HTTP worker pool, gofeed, HTML -> markdown
    +-- internal/opml         subscription import and export
    +-- internal/notify       notify-send / osascript
    +-- internal/theme        palettes
    +-- internal/ui           Bubble Tea model, passive ticker
             |
             +-- textutil     escape stripping, wrap, truncate, time
```

`theme` and `textutil` are leaves. `feed` parses into its own `Item` type;
`ui` maps that to `store.IngestArticle`. Persistence never leaks into parsing and
the parser never writes.

## Refresh sequence

```text
tick / r / startup
        |
        v
  refreshing = true, spinner on
        |
        v
  Feeds() -> cached ETag / Last-Modified per URL
        |
        v
  FetchAll(feeds, validators)  -- worker pool, per-feed context timeout
        |
        +--> 304       -> TouchFeed (clear error, adopt config title)
        +--> feed ok   -> SyncFeed (tx: upsert feed + validators, insert unseen)
        +--> feed fail -> RecordFeedError (no article row)
        |
        v
  Prune(max_articles_per_feed), SetMeta(last_updated)
        |
        v
  refreshDoneMsg -> load articles + feed health -> recompute -> render
        |
        v
  nextTick (scheduled from completion, not from this poll's start)
```

The fetch command runs off the UI goroutine. It writes to SQLite and returns a
message; the model never blocks on network or disk. Mark-read is a separate
command with its own 10s timeout. One connection and WAL mean those commands
serialise instead of fighting over a lock.

A slow feed is bounded twice: `http.Client.Timeout` and the per-feed
`context.WithTimeout`. The pool size bounds total in-flight work. A timeout
becomes `timed out after 20s` in the status bar and the error overlay, not a
stalled UI.

Conditional requests are a pure fast path. The stored `ETag` and `Last-Modified`
are replayed; a `304` skips the body read, parse, markdown conversion and
article upserts, and only refreshes the feed's health row. A feed that ignores
validators is unaffected, because a `200` still replaces both stored values.

The next timer is armed when a poll finishes, so the interval is an idle gap
rather than a start-to-start schedule. A poll that outlives its interval cannot
trigger a second poll immediately after it returns.

## Schema

```sql
feeds(id, url UNIQUE, title, category, last_fetched, last_error,
      etag, last_modified)
articles(id, feed_id, guid, title, link, author, summary, content,
         published, first_seen, is_read, starred,
         UNIQUE(feed_id, guid))
meta(key PRIMARY KEY, value)
articles_fts USING fts5(title, content, content='articles', content_rowid='id')
```

- `INSERT ... ON CONFLICT(feed_id, guid) DO NOTHING` makes re-fetch idempotent
  and preserves `is_read` and `starred`.
- Pruning uses `ROW_NUMBER() OVER (PARTITION BY feed_id ORDER BY published)` over
  unstarred rows only, so the cap bounds the transient cache and a starred
  article survives. `starred` is user intent, and `is_read` is triage state.
- `last_updated` lives in `meta`. The theme does not: `config.yaml` owns the
  user's settings, so the app reads the theme from YAML and writes it back
  through the comment-preserving saver. The app rewrites the config only for
  `import-opml` and a settings-menu change, and keeps a `.bak`.
- A `meta.theme` row from an older build is reconciled once on start: while the
  YAML still holds the default theme it is adopted into the config, and the key
  is deleted either way. A theme already chosen in YAML always wins.
- `PRAGMA user_version` guards against opening a newer schema with an older
  build. Version 1 to 2 adds the two validator columns with `ALTER TABLE` in one
  transaction. Version 2 to 3 adds `articles.starred` and its index the same
  way. Version 3 to 4 adds the FTS5 index and its triggers, then backfills it
  from the existing rows in the same transaction. A fresh database is created at
  the current version and skips the migration list. The upgrade path is tested
  against seeded v1, v2 and v3 databases, not only a fresh file.
- `articles_fts` is an external-content FTS5 table over `articles`, so the index
  stores tokens and not a second copy of every body. `title` and `content` are
  indexed. Triggers keep it in sync: insert and delete mirror the row, and
  `AFTER UPDATE OF title, content` reindexes only when indexed text changes, so
  marking an article read or starred does not churn the index. A cascading feed
  delete reaches the delete trigger, which is why pruning a feed leaves no search
  hits behind. `INSERT INTO articles_fts(articles_fts) VALUES('rebuild')` repairs
  a drifted index without a resync job.
- Feed health is read from the same rows the poll writes: `Feeds()` joins the
  article counts, so the `F` overlay and `e` errors show persisted state rather
  than this process's memory.

## Interactive state

```text
            Tab (manual single)
         +----------------------+
         |                      v
  [two-pane list focus] <-> [single pane list focus]
         | Enter                  | Enter
         v                        v
  [two-pane preview]  <-> [single pane preview]
         | Esc                    | Esc
         +------------------------+
```

- Focus chooses whether `j`/`k` moves the selection or scrolls the article.
- `Enter` marks read on first open. `Space` toggles read in place.
- Filters (`u`, `S`, `/`, `c`) rebuild the visible slice and reset the cursor to
  the top; a reload preserves the selected article by ID.
- `/` runs two paths on purpose. Headlines and feed names are filtered in memory
  with a fuzzy subsequence match, so the list reacts on the keystroke. Bodies
  need the store, so a query of two or more characters also schedules
  `SearchBodies`; the returned IDs widen the visible set when they arrive. A
  response whose query no longer matches the input is dropped, and a reload
  re-runs the search for the active query. A store-less model stays
  headline-only.
- `ftsQuery` quotes every term and stars the last one. Quoting means punctuation
  and FTS5 operators in the input are terms, not syntax, so a user query cannot
  raise a match error; the star makes live typing match as the word grows.
- Marking read does not remove the row from a live unread-only view. The next
  reload does. This avoids a cursor jump while reading.
- Layout answers one question: `single := width < 100 || manualSingle`. There is
  no second layout code path.
- `,` opens the settings overlay. Rows are theme, refresh interval,
  notifications, passive items, and passive unread only; `j`/`k` move and
  `h`/`l`, `Enter`, or `Space` change a row. A change applies to the live model
  at once and is saved on the spot, so leaving the menu never reverts it. Passive
  rows only affect the next `--passive` run, and the new interval arms the next
  tick rather than cancelling the pending one.
- Mouse events use the same pane geometry the renderer uses. A list click maps
  to `offset + y - 3` (border, header, blank), so it stays correct after a
  scroll; a click in the preview's columns focuses the preview. The wheel
  scrolls the focused pane, three rows per notch. A second click on the same
  cell within 400ms opens the article, which is `Enter`.

## Rendering and terminal safety

- Feed text is untrusted. `textutil.Clean` removes CSI, OSC and C0 control
  sequences before anything is measured or painted. A feed cannot inject
  terminal escapes.
- Every pane is built from lines measured in terminal cells (`runewidth`), then
  the lipgloss border and padding are added. Content width is `paneWidth - 4`
  (border 2 + padding 2), so a wide character cannot push the border out.
- Preview markdown is flattened to prose first: headings, emphasis, links and
  images are reduced, code fences kept verbatim. No markdown renderer is needed
  in the hot path.
- The preview body is wrapped once per article/width and cached, so scrolling
  does not re-wrap 20k characters per keystroke.
- Palette roles are the only colours the UI knows. No view hard-codes a hex
  value.
- A list row is read marker, read dot, headline, gap, compact age, and feed tag.
  The age drops below 60 columns so the headline and feed keep the space. A star
  marker rides inside the headline budget, so it truncates like the title.

## Passive redraw protocol

The block is printed with a trailing newline, so the cursor always rests one
line below it. A redraw moves up `prev` lines, clears each line with `\r\x1b[2K`,
prints the new block, and clears any lines the previous, larger block left
behind. Line width is capped at `terminalWidth - 1`; writing the last cell would
trigger a wrap and desynchronise the cursor arithmetic.

The block is capped at `height - 1` lines so a tall block cannot scroll the
terminal out from under the cursor. Failure rows are reserved before article
rows are laid out, so a short pane still says which feeds failed instead of
silently trimming that section. When a redraw shrinks the block, the extra rows
are cleared in the same pass, so no stale headline can survive below it.
`Finish` only moves to a clean line on exit; it never erases the final block.
Each cycle renders before it waits, so a cancellation during the wait drops no
rendered update. A cancellation during a fetch abandons that in-flight fetch
because the process is leaving. `SIGINT`/`SIGTERM` cancel the poll context
and leave a clean line. Non-TTY output skips every escape sequence and prints
once.

## Failure contract

| Path | Behaviour |
| --- | --- |
| Feed fetch | Per-feed timeout, isolated error, other feeds continue, retry next tick |
| Feed parse | Same; error recorded with the feed title |
| Cache write | One transaction per feed; error shown in status and `e`; next poll retries. A later prune or meta failure does not roll back articles already committed. |
| Browser launch | `tea.ExecProcess` releases the terminal; failure shown in status |
| Notification | Best effort, 5s timeout, missing binary ignored |
| Body search | 5s timeout; a failure is shown in the status bar and the list falls back to headline matches. A malformed MATCH is impossible because every term is quoted. |
| Settings save | Comment-preserving YAML rewrite; failure shown in status, the live value still applies for the session, and the next successful save retries the write |
| OPML import | Validate the merged config before writing; back up to `config.yaml.bak`, then write a temp file and rename. A failure leaves the live config untouched. |

## Testing

- Config: sample generation, duration parsing, defaults, dedupe, validation,
  merge-by-URL, backup and atomic save, and a comment-preserving save that keeps
  the feed block and appends a missing key before it.
- Store: idempotent upsert, read-state preservation, unread/category filters,
  pruning, cascade on feed removal, meta upsert, feed health counters, error
  adoption of renamed feeds, a v1-to-v2 and v2-to-v3 migration against seeded
  databases, a v3-to-v4 migration that backfills the FTS index, body search over
  title and content with prefix matching, index survival across read-state writes,
  index cleanup on a cascading feed prune, query sanitising for quotes,
  operators and punctuation-only input, and a rebuild after a deliberately
  cleared index.
- Feed: RSS and Atom parsing, relative link resolution, script stripping,
  HTML-to-markdown, timeout and HTTP errors, input-order preservation,
  conditional requests (`If-None-Match`, `If-Modified-Since`, 304, validator
  replacement when a feed changes).
- OPML: category grouping on export, nested and flat import, duplicate and
  lowercase-attribute handling, invalid XML.
- Text: escape stripping, wide-character truncation, wrapping, markdown
  flattening, relative timestamps.
- UI: filter combinations, pane layout at 90/120/160 columns, exact view height
  and width, row truncation, status contents, next-unread traversal, OSC 52
  payload, feed-health overlay, persisted-error seeding, passive block width and
  height caps with reserved failure rows, redraw cursor arithmetic, and a
  settings menu that changes the live model and reloads the same values from a
  temporary config file, plus mouse mapping for row selection after a scroll,
  preview focus, ignored clicks outside the rows, wheel clamping, and double
  click opening, and a body search that finds a body-only word through the store,
  drops a stale response, skips one-character queries, falls back without a
  store, flashes a search error, and re-runs on reload.
- Theme: every palette fills every role with a valid hex colour, cycle order in
  both directions, and a legacy `meta.theme` adopted into the YAML once.
