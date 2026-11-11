# Changelog

All notable changes to rss-tui are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-11-12

### Added

- `n` / `N` jump to the next or previous unread article, the triage keys a
  reader of this shape needs.
- `y` copies the article link and `Y` copies a markdown link through OSC 52, so
  the clipboard works over SSH and inside tmux without a new dependency.
- `F` opens a feed-health overlay listing every cached feed with its category,
  unread and cached counts, last successful poll, and last error.
- Conditional fetching: each feed's `ETag` and `Last-Modified` are stored and
  replayed, so a `304 Not Modified` skips the body read, parse, markdown
  conversion, and prune.
- `rss-tui export-opml` and `rss-tui import-opml` move subscriptions between
  readers. Import merges by URL by default (`--replace` starts clean), backs up
  the previous config to `config.yaml.bak`, and writes atomically.
- `passive_unread_only` config key, so the ticker can show only what is unread.
- GitHub Actions CI that runs gofmt, `go vet`, tests, the race detector, and a
  build, plus a pull-request gate on branch prefixes. `make build` stamps the
  version from `git describe`, and `make check` runs the full local gate.

### Changed

- Feed errors recorded in SQLite now surface after a restart, in the status bar
  and under `e`.
- The refresh timer is scheduled from the end of a poll rather than from its
  start, so a slow poll no longer causes back-to-back refreshes.
- The preview footer no longer advertises `j/k scroll` while that key moves the
  article cursor.
- Passive mode reserves room for failure rows before it trims articles to the
  pane height.

### Fixed

- `RecordFeedError` now adopts a feed's configured title and category, so a
  renamed feed is correct even while it keeps failing.
- A feed that returns `304` now clears its stored error and adopts config
  changes without discarding the title parsed from its last full response.

## [0.1.0] - 2026-11-12

### Added

- Interactive Bubble Tea reader with a two-pane list/preview layout, collapsing
  to a single pane below 100 columns or on `Tab`.
- Passive ticker mode (`--passive`, `-p`) that redraws a compact headline block
  in place without the alt screen, plus `--once` for cron-shaped runs.
- Concurrent feed fetching with a bounded worker pool, per-feed context
  timeouts, HTTP status handling, and an 8 MiB body cap.
- RSS and Atom parsing through `gofeed`, with HTML converted to markdown by
  `html-to-markdown` and flattened to prose for the preview.
- SQLite cache via pure-Go `modernc.org/sqlite`, with idempotent article
  upserts, preserved read state, per-feed pruning, WAL mode, and schema
  versioning.
- YAML configuration with categories, refresh interval, per-feed timeout,
  worker count, cache bound, browser override, and opt-in notifications.
  A four-feed sample config is written on first run.
- Vim-style navigation, live fuzzy headline search, All/Unread filter, category
  cycling, mark-all-visible-read, and an error overlay.
- Six IDE colour palettes (Catppuccin Mocha, Dracula, Nord, Gruvbox Dark, Tokyo
  Night, Solarized Dark) with live cycling on `t` and the choice persisted.
- Coalesced desktop notifications through `notify-send` or `osascript`, one per
  refresh cycle and off by default.
- Terminal-escape sanitisation for untrusted feed text, plus width-aware
  wrapping and truncation.
- Offline test suite covering config, store, feed parsing and timeouts, text
  shaping, palettes, notifications, layout, and passive redraw arithmetic.
