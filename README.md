# rss-tui

[![CI](https://github.com/ultus-net/rss-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/ultus-net/rss-tui/actions/workflows/ci.yml)

A fast, low-glare RSS/Atom reader for a dedicated terminal window or tmux pane.

Two modes: a full-screen Bubble Tea reader, and a passive ticker that redraws a
compact headline block in place. Both share one SQLite cache, so read state and
the last poll survive restarts.

## Status

Working v0.3. CI runs gofmt, `go vet`, the tests, the race detector, and a
build on every pull request. The interactive UI and passive mode were
smoke-tested in tmux against live feeds.

## Requirements

- Go 1.27 or newer to build. The module pins `go 1.27.3`.
- Linux or macOS. Linux uses `notify-send` for optional notifications and
  `xdg-open` as the default browser; macOS uses `osascript` and `open`.
- A terminal with 256-colour support for the themes. `NO_COLOR` disables colour.

## Build and run

One command, no install step:

```sh
go run ./cmd/rss-tui --once
```

Build a binary:

```sh
go build -o rss-tui ./cmd/rss-tui
./rss-tui              # interactive
./rss-tui --passive    # ticker (alias: -p)
```

On first run rss-tui writes a sample config with four tech/news feeds, fetches
them, and caches the articles. Delete the config to regenerate the sample.

```sh
make build   # ./rss-tui, version stamped from git describe
make test    # offline test suite
make check   # gofmt check + vet + tests + race detector + build
```

`./rss-tui --version` prints the stamped version. A plain `go build` leaves it
as `dev`.

## Modes

| Mode | Flag | Behaviour |
| --- | --- | --- |
| Interactive | default | Alt-screen two-pane reader. Polls on a timer and on `r`. |
| Passive | `--passive`, `-p` | Prints a headline block and redraws it in place after each poll. No alt screen. |
| Once | `--once` | Fetches once, prints the block, exits. Useful from cron. |

Passive mode prints one plain block and exits when stdout is not a terminal, so
piping it into a log file behaves sensibly. `--once` is implied there.

```text
rss-tui · 14:02 · 3 new · 12 unread · 4 feeds · refresh 10m
──────────────────────────────────────────────────────────────
14:01  Hacker News    ● Show HN: A terminal feed reader
13:48  BBC World      ● Hundreds of schools hit by hoax threats
13:31  Lobsters       · Using voltage glitching on a mining card
```

## Keybindings

| Key | Action |
| --- | --- |
| `j` / `↓` | Next article, or scroll the preview when it has focus |
| `k` / `↑` | Previous article, or scroll the preview |
| `n` / `N` | Next / previous unread article |
| `g` / `G` | First / last article (or top / bottom of preview) |
| `Ctrl+d` / `Ctrl+u` | Half page down / up (also `PgDn` / `PgUp`) |
| `h` / `l` | Focus list / preview pane (also `←` / `→`) |
| `Enter` | Open the preview and mark the article read; `Enter` again or `Esc` returns to the list |
| `Space` | Toggle read / unread without leaving the list |
| `s` | Star / unstar the selected article |
| `S` | Toggle the starred-only filter |
| `o` | Open the article link in the configured browser |
| `y` / `Y` | Copy the link / copy a markdown link to the terminal clipboard |
| `r` | Refresh every feed now |
| `u` | Toggle All / Unread-only filter |
| `/` | Live fuzzy search over visible headlines |
| `m` | Mark every visible article read |
| `c` | Cycle category filter: All → each category → All |
| `t` | Cycle colour theme; saved to `config.yaml` |
| `,` | Settings menu: theme, refresh interval, notifications, passive rows |
| `Tab` | Force single-pane mode at any width |
| `F` | Feed health: counts, last poll, and last error per feed |
| `e` | Show feed error details, including errors from earlier runs |
| `?` | Help overlay (any key closes it) |
| `q` / `Ctrl+c` | Quit |

Search: typing filters as you type, `Enter` keeps the filter, `Esc` clears it.
Under the unread-only filter an article you just read stays visible until the
next refresh, so the cursor does not jump out from under you.

A star is user intent, not cache. The per-feed article cap counts only unstarred
rows, so a starred article survives pruning however old it gets. Removing a feed
from the config still drops its articles, starred or not, because the config is
the source of truth. Stars are local: they are not published back to the feed.

`y` and `Y` use OSC 52, so the clipboard works inside tmux and over SSH. In
tmux, enable `set -g set-clipboard on` for the sequence to reach the outer
terminal. Terminals without clipboard support ignore it silently.

## Layout

At 100 columns or wider the list and preview sit side by side. Narrower
terminals, including small tmux panes, collapse to a single pane: `Enter` opens
the article full-width and `Esc` returns to the list. `Tab` forces single-pane
mode on a wide terminal.

A list row shows read state, the headline, a compact age stamp (`5m`, `3h`, `2d`,
`2 Jan`), and the feed. The age hides below 60 columns so the headline keeps the
space.

## Configuration

Config lives at `~/.config/rss-tui/config.yaml` (respects `XDG_CONFIG_HOME`).
Unknown keys are rejected so typos fail loudly. Durations use Go syntax.

```yaml
refresh_interval: 10m        # 30s .. 24h
per_feed_timeout: 20s        # 1s .. 5m
concurrent_fetches: 6        # 1 .. 32
max_articles_per_feed: 100   # cache bound per feed
passive_items: 20            # headlines shown by --passive
passive_unread_only: false   # hide read headlines in --passive

theme: catppuccin-mocha
browser: ""                  # empty = xdg-open / open
notifications: false         # opt-in desktop notifications

feeds:
  - url: https://news.ycombinator.com/rss
    title: Hacker News
    category: tech
  - url: https://feeds.bbci.co.uk/news/world/rss.xml
    title: BBC World
    category: news
```

- `title` is optional; the feed's own title is used, then the URL host.
- `category` groups feeds. An omitted category becomes `general`. `c` in the UI
  cycles the categories in config order.
- `browser` is split on whitespace and the article URL is appended, so
  `browser: "firefox --new-tab"` and `browser: "w3m"` both work. Terminal
  browsers release the TUI, run in the terminal, and return cleanly.
- `concurrent_fetches` feeds are in flight at once. Each feed gets its own
  timeout.
- rss-tui stores each feed's `ETag` and `Last-Modified` and replays them. A feed
  that answers `304 Not Modified` costs one small response and no parse.
- Removing a feed from config drops its cached articles and read state on the
  next start. The config file is the source of truth for subscriptions.
- `,` opens the settings menu. A change applies live and is written to
  `config.yaml` immediately: comments and the feed block are kept, and the
  previous file stays at `config.yaml.bak`. Editing feeds remains a file job and
  `import-opml`.
- The YAML `theme` is authoritative. On the first run of this version, if the
  file still holds the default theme, a theme left in SQLite by an older build is
  written into `config.yaml`. The old key is dropped either way.

### OPML import and export

Move subscriptions to or from any other feed reader:

```sh
# Write every configured feed to stdout, grouped by category.
rss-tui export-opml > subscriptions.opml
rss-tui export-opml -o subscriptions.opml

# Merge a subscriptions.opml into config.yaml, keeping existing feeds.
rss-tui import-opml subscriptions.opml

# Replace the feed list with the file's contents.
rss-tui import-opml --replace subscriptions.opml
```

Import merges by URL, skips feeds that are already configured, and reports the
counts. It writes `config.yaml.bak` first and replaces the config atomically, so
an interrupted import cannot truncate it. Nested OPML outlines become
categories, and both the `xmlUrl` and `xmlurl` spellings are accepted. Replaced
feeds lose their cached articles on the next start, because config is the source
of truth.

### Themes

Six IDE palettes ship, selected by `theme`, rotated in the settings menu, or
cycled with `t`:

| Name | Palette |
| --- | --- |
| `catppuccin-mocha` | Catppuccin Mocha (default) |
| `dracula` | Dracula |
| `gruvbox-dark` | Gruvbox Dark |
| `nord` | Nord |
| `tokyo-night` | Tokyo Night |
| `solarized-dark` | Solarized Dark |

Each palette fills the same semantic roles: unread accent, read/muted text,
faint hints, borders, selection, status bar, error, warning, search, source tag.
Adding a palette is one struct in `internal/theme`, not a UI change. `--theme`
overrides the config for one run and does not persist.

Read articles dim to the muted colour; unread dots and the active pane border
take the accent. The status bar stays low-contrast for an always-open window.

### Notifications

Notifications are off by default. Set `notifications: true` to get at most one
desktop notification per refresh cycle, never one per article:

```text
3 new articles
Hacker News, Lobsters, BBC World
Go 1.27 released
```

Linux uses `notify-send` (urgency low, 8s expiry). macOS uses `osascript`. A
missing notification daemon is ignored rather than reported as an error.

## Files

| Path | Contents |
| --- | --- |
| `~/.config/rss-tui/config.yaml` | Feeds, categories, intervals, theme, browser |
| `~/.local/share/rss-tui/state.db` | SQLite cache: articles, read state, feed errors, last poll |

`XDG_CONFIG_HOME` and `XDG_DATA_HOME` are honoured. SQLite runs in WAL mode with
a single connection, so the UI and background polls never hold a write lock
open.

## How it works

- `internal/config` loads YAML, applies defaults, validates ranges and URLs,
  writes the sample on first run, and merges or saves configs for OPML import.
- `internal/feed` runs a bounded goroutine pool. Each feed gets
  `context.WithTimeout`, a custom User-Agent, an 8 MiB body cap, and cached
  cache validators. Parsed HTML becomes markdown through `html-to-markdown`,
  then plain text for the preview.
- `internal/opml` reads and writes the subscription format, one nesting level
  per category.
- `internal/store` owns SQLite. Articles upsert on `(feed_id, guid)`, so a
  re-fetch never resets read state. Pruning keeps the newest N per feed.
- `internal/ui` holds both front ends. The interactive model owns filters,
  layout, theme and key handling; passive mode reuses the fetcher and store.
- `internal/textutil` strips ANSI/OSC escapes and control characters from
  untrusted feed text, then wraps and truncates by terminal cell width.
- `internal/notify` sends the coalesced notification; `internal/theme` holds the
  palettes.

Feed failures are isolated: a timeout or HTTP error marks that feed failed, the
other feeds still land, and the status bar shows a warning with details under
`e`. Errors are stored per feed, so they are still there after a restart and in
the `F` health view. Every poll retries. There is no dead-letter queue because
feeds are read-only and idempotent. Cache writes happen in one SQLite
transaction per feed; a failed write is surfaced in the status bar and the next
poll retries.

See [docs/DESIGN.md](docs/DESIGN.md) for the design decisions, schema, and
refresh sequence.

## Testing

The suite is offline. Feed parsing and timeouts run against `httptest` servers;
the store uses a temporary SQLite file.

```sh
make check    # gofmt check, vet, tests, race detector, build
```

Individual steps:

```sh
go test ./...
go vet ./...
gofmt -l ./cmd ./internal   # empty when formatted
```

Release steps live in [docs/RELEASING.md](docs/RELEASING.md).

## Limitations

- No article images, only text.
- No full-text search over article bodies; `/` matches headlines and feed names.
- Passive mode polls at `refresh_interval`; it does not stream.
- `--once` is a cron-shaped entry point, not a daemon.
- Windows builds compile, but only Linux and macOS are smoke-tested.

## Licence

MIT. See [LICENSE](LICENSE).
