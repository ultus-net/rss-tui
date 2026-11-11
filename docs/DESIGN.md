# rss-tui design

## Intent

Keep a feed reader open in a dedicated terminal or tmux pane without it
becoming another browser tab. It must be cheap to leave running, keyboard-first,
and quiet unless something happened.

Non-goals: a web UI, article archiving, full-text search across bodies, image
rendering, or an account system. The reader reads feeds and tracks what you have
seen.

## Decisions

Four choices were settled with the operator before implementation.

| Question | Decision | Why |
| --- | --- | --- |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no CGO. Indexed unread counts and atomic read-state writes stay fast with thousands of cached articles. A JSON file rewrites everything on each mark-read. |
| Notifications | Opt-in desktop notification, coalesced per cycle | Quiet first run. One popup per poll, never one per article. Missing daemon is ignored. |
| Themes | Six IDE palettes, `t` cycles live, choice persisted | Palettes are data, not UI code. Live cycling makes a low-glare choice cheap to find. |
| Narrow terminals | Single pane under 100 cols, `Tab` forces it | Small tmux panes are the primary use case. Squeezing two panes there helps nobody. |

Alternatives rejected: a single-file JSON state (write amplification), an
always-on notification stream (noise), a config-only theme switch (slow to
evaluate), and keeping the split at every width (unreadable below ~90 cols).

## Packages and dependency direction

```text
cmd/rss-tui
    |
    +-- internal/config       YAML, defaults, validation, XDG paths
    +-- internal/store        SQLite schema and queries
    +-- internal/feed         HTTP worker pool, gofeed, HTML -> markdown
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
  FetchAll(feeds)  -- worker pool, per-feed context timeout
        |
        +--> feed ok   -> SyncFeed (tx: upsert feed, insert unseen) -> new count
        +--> feed fail -> RecordFeedError (no article row)
        |
        v
  Prune(max_articles_per_feed), SetMeta(last_updated)
        |
        v
  refreshDoneMsg -> load articles -> recompute filters -> render
```

The fetch command runs off the UI goroutine. It writes to SQLite and returns a
message; the model never blocks on network or disk. Mark-read is a separate
command with its own 10s timeout. One connection and WAL mean those commands
serialise instead of fighting over a lock.

A slow feed is bounded twice: `http.Client.Timeout` and the per-feed
`context.WithTimeout`. The pool size bounds total in-flight work. A timeout
becomes `timed out after 20s` in the status bar and the error overlay, not a
stalled UI.

## Schema

```sql
feeds(id, url UNIQUE, title, category, last_fetched, last_error)
articles(id, feed_id, guid, title, link, author, summary, content,
         published, first_seen, is_read,
         UNIQUE(feed_id, guid))
meta(key PRIMARY KEY, value)
```

- `INSERT ... ON CONFLICT(feed_id, guid) DO NOTHING` makes re-fetch idempotent
  and preserves `is_read`.
- Pruning uses `ROW_NUMBER() OVER (PARTITION BY feed_id ORDER BY published)` and
  keeps the newest `max_articles_per_feed`.
- `theme` and `last_updated` live in `meta`. The config file stays user-authored
  YAML; the app never rewrites it.
- `PRAGMA user_version` guards against opening a newer schema with an older
  build.

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
- Filters (`u`, `/`, `c`) rebuild the visible slice and reset the cursor to the
  top; a reload preserves the selected article by ID.
- Marking read does not remove the row from a live unread-only view. The next
  reload does. This avoids a cursor jump while reading.
- Layout answers one question: `single := width < 100 || manualSingle`. There is
  no second layout code path.

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

## Passive redraw protocol

The block is printed with a trailing newline, so the cursor always rests one
line below it. A redraw moves up `prev` lines, clears each line with `\r\x1b[2K`,
prints the new block, and clears any lines the previous, larger block left
behind. Line width is capped at `terminalWidth - 1`; writing the last cell would
trigger a wrap and desynchronise the cursor arithmetic.

The block is capped at `height - 1` lines so a tall block cannot scroll the
terminal out from under the cursor. When a redraw shrinks the block, the extra
rows are cleared in the same pass, so no stale headline can survive below it.
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
| Theme persist | Best effort; failure shown, theme still applied for the session |

## Testing

- Config: sample generation, duration parsing, defaults, dedupe, validation.
- Store: idempotent upsert, read-state preservation, unread/category filters,
  pruning, cascade on feed removal, meta upsert.
- Feed: RSS and Atom parsing, relative link resolution, script stripping,
  HTML-to-markdown, timeout and HTTP errors, input-order preservation.
- Text: escape stripping, wide-character truncation, wrapping, markdown
  flattening, relative timestamps.
- UI: filter combinations, pane layout at 90/120/160 columns, exact view height
  and width, row truncation, status contents, passive block width and height
  caps, redraw cursor arithmetic.
- Theme: every palette fills every role with a valid hex colour.
