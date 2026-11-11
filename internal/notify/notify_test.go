package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	title, body := Format(NewArticles{Count: 1, Feeds: []string{"Hacker News"}, Titles: []string{"Go 1.27 released"}})
	if title != "1 new article" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(body, "Hacker News") || !strings.Contains(body, "Go 1.27 released") {
		t.Fatalf("body = %q", body)
	}

	title, body = Format(NewArticles{Count: 5, Feeds: []string{"a", "b", "c", "d", "e"}})
	if title != "5 new articles" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(body, "a, b, c and 2 more") {
		t.Fatalf("body = %q", body)
	}

	title, body = Format(NewArticles{Count: 2})
	if title != "2 new articles" || body == "" {
		t.Fatalf("empty payload: %q / %q", title, body)
	}
}

func TestDisabledIsNoop(t *testing.T) {
	n := &Notifier{
		enabled: false,
		goos:    "linux",
		look:    func(string) (string, error) { t.Fatal("lookup should not run"); return "", nil },
		run:     func(context.Context, string, ...string) error { t.Fatal("run should not run"); return nil },
	}
	if err := n.Send(context.Background(), NewArticles{Count: 3}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n.Enabled() {
		t.Fatal("Enabled = true")
	}
}

func TestMissingNotifierIsUnavailable(t *testing.T) {
	n := &Notifier{
		enabled: true,
		goos:    "linux",
		look:    func(string) (string, error) { return "", errors.New("missing") },
		run:     func(context.Context, string, ...string) error { t.Fatal("run should not run"); return nil },
	}
	if err := n.Send(context.Background(), NewArticles{Count: 1}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestLinuxInvocation(t *testing.T) {
	var gotName string
	var gotArgs []string
	n := &Notifier{
		enabled: true,
		goos:    "linux",
		look:    func(string) (string, error) { return "/usr/bin/notify-send", nil },
		run: func(_ context.Context, name string, args ...string) error {
			gotName = name
			gotArgs = args
			return nil
		},
	}
	if err := n.Send(context.Background(), NewArticles{Count: 2, Feeds: []string{"Feed"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotName != "notify-send" {
		t.Fatalf("binary = %q", gotName)
	}
	if len(gotArgs) < 3 || gotArgs[0] != "--app-name=rss-tui" {
		t.Fatalf("args = %v", gotArgs)
	}
	if gotArgs[len(gotArgs)-2] != "2 new articles" {
		t.Fatalf("title arg = %q (args %v)", gotArgs[len(gotArgs)-2], gotArgs)
	}
}

func TestDarwinQuoting(t *testing.T) {
	if got := osascriptQuote(`a "b" \ c`); got != `"a \"b\" \\ c"` {
		t.Fatalf("quote = %s", got)
	}
}
