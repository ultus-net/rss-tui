# Changelog

All notable changes to rss-tui are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

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
