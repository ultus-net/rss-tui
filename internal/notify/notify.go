// Package notify sends one desktop notification per refresh cycle, on demand.
package notify

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrUnavailable means no supported notifier was found on this system.
var ErrUnavailable = errors.New("notify: no desktop notifier available")

// Runner executes a notifier binary.
type Runner func(ctx context.Context, name string, args ...string) error

// NewArticles describes one coalesced notification.
type NewArticles struct {
	Count  int
	Feeds  []string
	Titles []string
}

// Notifier shells out to notify-send (Linux) or osascript (macOS). It is off by
// default and never blocks the UI: callers run it in a goroutine.
type Notifier struct {
	enabled bool
	goos    string
	look    func(string) (string, error)
	run     Runner
}

func New(enabled bool) *Notifier {
	return &Notifier{
		enabled: enabled,
		goos:    runtime.GOOS,
		look:    exec.LookPath,
		run: func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.Run()
		},
	}
}

func (n *Notifier) Enabled() bool { return n != nil && n.enabled }

// Send fires one notification. A missing notifier returns ErrUnavailable so
// callers can stay quiet instead of nagging.
func (n *Notifier) Send(ctx context.Context, news NewArticles) error {
	if !n.Enabled() || news.Count <= 0 {
		return nil
	}
	title, body := Format(news)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch n.goos {
	case "linux":
		if _, err := n.look("notify-send"); err != nil {
			return ErrUnavailable
		}
		return n.run(ctx, "notify-send",
			"--app-name=rss-tui", "--urgency=low", "--expire-time=8000", title, body)
	case "darwin":
		if _, err := n.look("osascript"); err != nil {
			return ErrUnavailable
		}
		script := fmt.Sprintf(`display notification %s with title %s`,
			osascriptQuote(body), osascriptQuote(title))
		return n.run(ctx, "osascript", "-e", script)
	default:
		return ErrUnavailable
	}
}

// Format builds the notification title and body. Kept pure for tests.
func Format(news NewArticles) (title, body string) {
	title = fmt.Sprintf("%d new articles", news.Count)
	if news.Count == 1 {
		title = "1 new article"
	}
	parts := make([]string, 0, 2)
	if len(news.Feeds) > 0 {
		feeds := news.Feeds
		if len(feeds) > 3 {
			feeds = feeds[:3]
			parts = append(parts, strings.Join(feeds, ", ")+fmt.Sprintf(" and %d more", len(news.Feeds)-3))
		} else {
			parts = append(parts, strings.Join(feeds, ", "))
		}
	}
	if len(news.Titles) > 0 && strings.TrimSpace(news.Titles[0]) != "" {
		parts = append(parts, strings.TrimSpace(news.Titles[0]))
	}
	body = strings.Join(parts, "\n")
	if body == "" {
		body = "Open rss-tui to read them."
	}
	return title, body
}

func osascriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}
