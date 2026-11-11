package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/ultus-net/rss-tui/internal/notify"
	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/textutil"
	"github.com/ultus-net/rss-tui/internal/theme"
)

// PassiveOptions configures the ticker mode.
type PassiveOptions struct {
	Once  bool
	Out   io.Writer
	Width int
	Color bool
}

type passiveView struct {
	Now       time.Time
	Updated   time.Time
	NewCount  int
	Unread    int
	FeedCount int
	Feeds     []string
	Failures  []feedError
	Articles  []store.Article
	Refresh   time.Duration
	Width     int
	Height    int
	Color     bool
	Palette   theme.Palette
}

// RunPassive polls all feeds on a timer and redraws a compact block in place.
// It never takes over the alt screen, so it is safe in a tmux pane. When stdout
// is not a terminal it prints one block and exits.
func RunPassive(ctx context.Context, deps Deps, opts PassiveOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	width, height, tty := terminalSize(out)
	if opts.Width > 0 {
		width = opts.Width
		tty = false
	}
	if width <= 0 {
		width = 100
	}
	color := opts.Color && tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	interactive := tty && !opts.Once
	renderer := &blockRenderer{out: out, interactive: interactive}
	once := opts.Once || !tty

	for {
		results := deps.Fetcher.FetchAll(ctx, deps.Cfg.Feeds)
		if ctx.Err() != nil {
			return nil
		}
		now := time.Now()
		newTotal := 0
		var failures []feedError
		newFeeds := make([]string, 0, len(results))
		newTitles := make([]string, 0, len(results))

		for _, r := range results {
			if r.Err != nil {
				failures = append(failures, feedError{Feed: r.Title, Err: r.Err.Error(), At: now})
				_ = deps.Store.RecordFeedError(ctx, r.Feed.URL, r.Title, r.Feed.Category, r.Err.Error())
				continue
			}
			added, err := deps.Store.SyncFeed(ctx, toIngest(r))
			if err != nil {
				failures = append(failures, feedError{Feed: r.Title, Err: err.Error(), At: now})
				continue
			}
			if added > 0 {
				newTotal += added
				newFeeds = append(newFeeds, r.Title)
				if len(r.Items) > 0 {
					newTitles = append(newTitles, r.Items[0].Title)
				}
			}
		}
		_ = deps.Store.Prune(ctx, deps.Cfg.MaxArticlesPerFeed)
		_ = deps.Store.SetMeta(ctx, "last_updated", strconv.FormatInt(now.Unix(), 10))

		articles, err := deps.Store.Articles(ctx, store.ListOptions{Limit: deps.Cfg.PassiveItems})
		if err != nil {
			return fmt.Errorf("passive: load articles: %w", err)
		}
		unread, err := deps.Store.UnreadCount(ctx)
		if err != nil {
			return fmt.Errorf("passive: unread count: %w", err)
		}
		if tty {
			if w, h, ok := terminalSize(out); ok {
				width, height = w, h
			}
		}

		lines := buildPassiveLines(passiveView{
			Now:       now,
			Updated:   now,
			NewCount:  newTotal,
			Unread:    unread,
			FeedCount: len(deps.Cfg.Feeds),
			Feeds:     newFeeds,
			Failures:  failures,
			Articles:  articles,
			Refresh:   deps.Cfg.RefreshInterval.Std(),
			Width:     width,
			Height:    height,
			Color:     color,
			Palette:   deps.Theme,
		})
		renderer.Render(lines)

		if newTotal > 0 && deps.Notifier != nil && deps.Notifier.Enabled() {
			news := notify.NewArticles{Count: newTotal, Feeds: newFeeds, Titles: newTitles}
			go func() {
				_ = deps.Notifier.Send(context.Background(), news)
			}()
		}
		if once {
			return nil
		}
		select {
		case <-ctx.Done():
			renderer.Finish()
			return nil
		case <-time.After(deps.Cfg.RefreshInterval.Std()):
		}
	}
}

func terminalSize(out io.Writer) (width, height int, tty bool) {
	f, ok := out.(*os.File)
	if !ok {
		return 0, 0, false
	}
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return 0, 0, false
	}
	w, h, err := term.GetSize(fd)
	if err != nil {
		return 0, 0, true
	}
	return w, h, true
}

type blockRenderer struct {
	out         io.Writer
	prev        int
	interactive bool
}

// Render clears the previous block in place. The cursor always ends on the line
// below the block so the next clear lands correctly. A non-interactive writer
// gets one plain block with no escape sequences.
func (r *blockRenderer) Render(lines []string) {
	if !r.interactive {
		fmt.Fprint(r.out, strings.Join(lines, "\n")+"\n")
		return
	}
	if r.prev > 0 {
		fmt.Fprintf(r.out, "\x1b[%dA", r.prev)
	}
	rows := r.prev
	if len(lines) > rows {
		rows = len(lines)
	}
	for i := 0; i < rows; i++ {
		fmt.Fprint(r.out, "\r\x1b[2K")
		if i < len(lines) {
			fmt.Fprint(r.out, lines[i])
		}
		fmt.Fprint(r.out, "\n")
	}
	if rows > len(lines) {
		fmt.Fprintf(r.out, "\x1b[%dA", rows-len(lines))
	}
	r.prev = len(lines)
}

// Finish leaves the terminal on a clean line after the block.
func (r *blockRenderer) Finish() {
	if r.prev > 0 {
		fmt.Fprint(r.out, "\n")
	}
}

func buildPassiveLines(v passiveView) []string {
	width := v.Width
	if width < 20 {
		width = 20
	}
	limit := width - 1 // never write the last cell, which would wrap the cursor

	accent := func(s string) string { return paint(s, v.Palette.Accent, v.Color) }
	alt := func(s string) string { return paint(s, v.Palette.AccentAlt, v.Color) }
	fg := func(s string) string { return paint(s, v.Palette.Fg, v.Color) }
	muted := func(s string) string { return paint(s, v.Palette.Muted, v.Color) }
	faint := func(s string) string { return paint(s, v.Palette.Faint, v.Color) }
	failure := func(s string) string { return paint(s, v.Palette.Error, v.Color) }

	dots := faint(" · ")
	headerParts := []string{
		accent("rss-tui"),
		faint(textutil.Clock(v.Now)),
	}
	if v.NewCount > 0 {
		headerParts = append(headerParts, accent(fmt.Sprintf("%d new", v.NewCount)))
	}
	headerParts = append(headerParts,
		muted(fmt.Sprintf("%d unread", v.Unread)),
		muted(fmt.Sprintf("%d feeds", v.FeedCount)),
		muted("refresh "+shortDuration(v.Refresh)),
	)
	if len(v.Failures) > 0 {
		headerParts = append(headerParts, failure(fmt.Sprintf("%d failed", len(v.Failures))))
	}
	lines := []string{truncateStyled(strings.Join(headerParts, dots), limit)}
	lines = append(lines, faint(strings.Repeat("─", limit)))

	tagW := width / 6
	if tagW < 8 {
		tagW = 8
	}
	if tagW > 14 {
		tagW = 14
	}
	const timeW = 5
	prefixW := timeW + 2 + tagW + 2 + 2
	titleW := limit - prefixW
	if titleW < 8 {
		titleW = 8
	}

	for _, a := range v.Articles {
		stamp := a.Published
		if stamp.IsZero() {
			stamp = a.FirstSeen
		}
		dot := accent("●")
		titleStyle := fg
		if a.Read {
			dot = faint("·")
			titleStyle = muted
		}
		tag := textutil.Truncate(a.FeedTitle, tagW)
		line := faint(textutil.Clock(stamp)) + "  " +
			alt(padRight(tag, tagW)) + "  " +
			dot + " " +
			titleStyle(textutil.Truncate(a.Title, titleW))
		lines = append(lines, truncateStyled(line, limit))
	}
	if len(v.Articles) == 0 {
		lines = append(lines, faint("no articles yet · waiting for the next refresh"))
	}

	if len(v.Failures) > 0 {
		lines = append(lines, "")
		for i, fe := range v.Failures {
			if i >= 2 {
				lines = append(lines, failure(fmt.Sprintf("… %d more failed", len(v.Failures)-2)))
				break
			}
			msg := fe.Feed + ": " + fe.Err
			lines = append(lines, truncateStyled(failure("⚠ "+msg), limit))
		}
	}

	if v.Height > 0 {
		maxLines := v.Height - 1
		if maxLines < 3 {
			maxLines = 3
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
	}
	return lines
}

func paint(s, color string, enabled bool) string {
	if !enabled {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}

func truncateStyled(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

func shortDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}
