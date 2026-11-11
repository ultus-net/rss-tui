package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/theme"
)

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		query, text string
		want        bool
	}{
		{"", "anything", true},
		{"go", "Go 1.27 released", true},
		{"gore", "Go release", true},
		{"gor", "Go release", true},
		{"go z", "Go release", false},
		{"日本", "日本語のニュース", true},
		{"NEWS", "daily news", true},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.query, c.text); got != c.want {
			t.Fatalf("fuzzyMatch(%q, %q) = %v, want %v", c.query, c.text, got, c.want)
		}
	}
}

func testModel(width, height int) Model {
	cfg := &config.Config{
		RefreshInterval: config.Duration(10 * time.Minute),
		Feeds: []config.Feed{
			{URL: "https://example.com/rss", Title: "Example", Category: "tech"},
			{URL: "https://news.example/rss", Title: "News", Category: "news"},
		},
	}
	m := New(Deps{Cfg: cfg, Theme: theme.Get("catppuccin-mocha")})
	m.width = width
	m.height = height
	m.ready = true
	now := time.Now()
	m.articles = []store.Article{
		{
			ID: 1, FeedID: 1, Title: "Go 1.27 released with faster builds",
			Link: "https://example.com/go", Summary: "Go release",
			Content:   "# Go 1.27\n\nThe compiler is **faster** now.\n\n" + strings.Repeat("Long body text. ", 80),
			Published: now.Add(-time.Hour), FeedTitle: "Example", FeedCategory: "tech",
		},
		{
			ID: 2, FeedID: 2, Title: "World news roundup",
			Link: "https://news.example/world", Summary: "World news",
			Content: "A short article.", Published: now.Add(-2 * time.Hour),
			FeedTitle: "News", FeedCategory: "news", Read: true,
		},
		{
			ID: 3, FeedID: 2, Title: "Go tooling in the news",
			Link: "https://news.example/go", Summary: "Go tooling",
			Content: "Another article.", Published: now.Add(-3 * time.Hour),
			FeedTitle: "News", FeedCategory: "news",
		},
	}
	m.recompute()
	return m
}

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}, {160, 40}} {
		m := testModel(size[0], size[1])
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Fatalf("%dx%d: view lines = %d", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > m.width {
				t.Fatalf("%dx%d line %d width %d > %d: %q", size[0], size[1], i, w, m.width, line)
			}
		}
	}
}

func TestNarrowTerminalUsesSinglePane(t *testing.T) {
	m := testModel(90, 24)
	if !m.singlePane() {
		t.Fatal("90 columns should be single pane")
	}
	if view := m.View(); !strings.Contains(view, "All articles") {
		t.Fatalf("list view missing header:\n%s", view)
	}
	m.focus = focusPreview
	view := m.View()
	if !strings.Contains(view, "esc back") {
		t.Fatalf("preview footer missing:\n%s", view)
	}
	if !strings.Contains(view, "Go 1.27") {
		t.Fatalf("preview title missing:\n%s", view)
	}
}

func TestWideTerminalSplitsPanes(t *testing.T) {
	m := testModel(120, 30)
	if m.singlePane() {
		t.Fatal("120 columns should be two panes")
	}
	view := m.View()
	if !strings.Contains(view, "All articles") || !strings.Contains(view, "enter open") {
		t.Fatalf("both panes should render:\n%s", view)
	}
	m.manualSingle = true
	if !m.singlePane() {
		t.Fatal("tab toggle should force single pane")
	}
}

func TestRecomputeAppliesFilters(t *testing.T) {
	m := testModel(120, 30)
	if len(m.visible) != 3 {
		t.Fatalf("all = %d", len(m.visible))
	}
	m.unreadOnly = true
	m.recompute()
	if len(m.visible) != 2 {
		t.Fatalf("unread = %d", len(m.visible))
	}
	m.query = "go"
	m.recompute()
	if len(m.visible) != 2 {
		t.Fatalf("unread+go = %d", len(m.visible))
	}
	m.category = "news"
	m.recompute()
	if len(m.visible) != 1 || m.visible[0].ID != 3 {
		t.Fatalf("news+go = %+v", m.visible)
	}
	m.unreadOnly = false
	m.query = ""
	m.category = ""
	m.recompute()
	if len(m.visible) != 3 {
		t.Fatalf("cleared filters = %d", len(m.visible))
	}
}

func TestRowWidth(t *testing.T) {
	article := store.Article{
		Title:     "A very long headline that should be truncated 日本語 too",
		FeedTitle: "Example Feed With A Long Name", FeedCategory: "tech",
	}
	st := newStyles(theme.Get("nord"))
	for _, width := range []int{20, 40, 71} {
		line := renderRow(article, width, true, st)
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("width %d: rendered %d: %q", width, got, line)
		}
	}
}

func TestStatusBarShowsCounts(t *testing.T) {
	m := testModel(120, 30)
	lines := strings.Split(m.View(), "\n")
	status := lines[len(lines)-1]
	if !strings.Contains(status, "2 unread") {
		t.Fatalf("status = %q", status)
	}
	if !strings.Contains(status, "? help") {
		t.Fatalf("status = %q", status)
	}
	if got := ansi.StringWidth(status); got != m.width {
		t.Fatalf("status width = %d, want %d", got, m.width)
	}
}

func TestSearchModeRendersPrompt(t *testing.T) {
	m := testModel(120, 30)
	m.searching = true
	m.query = "go"
	status := m.renderStatus(newStyles(m.theme))
	if !strings.Contains(status, "/go") || !strings.Contains(status, "esc clear") {
		t.Fatalf("search status = %q", status)
	}
}

func TestPassiveBlockFitsWidth(t *testing.T) {
	now := time.Now()
	v := passiveView{
		Now: now, Updated: now, NewCount: 2, Unread: 5, FeedCount: 3,
		Failures: []feedError{{Feed: "Broken", Err: "timed out after 20s"}},
		Articles: []store.Article{
			{Title: strings.Repeat("A long headline ", 8), FeedTitle: "Example", Published: now, FeedCategory: "tech"},
			{Title: "Read article", FeedTitle: "News", Published: now.Add(-time.Hour), FeedCategory: "news", Read: true},
		},
		Refresh: 10 * time.Minute,
		Width:   60,
		Height:  12,
		Palette: theme.Get("dracula"),
		Color:   false,
	}
	lines := buildPassiveLines(v)
	if len(lines) > v.Height-1 {
		t.Fatalf("lines = %d, height cap %d", len(lines), v.Height-1)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > v.Width-1 {
			t.Fatalf("line %d width %d: %q", i, got, line)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "5 unread") || !strings.Contains(joined, "2 new") {
		t.Fatalf("header missing counts:\n%s", joined)
	}
	if !strings.Contains(joined, "Broken: timed out") {
		t.Fatalf("failure missing:\n%s", joined)
	}
	if !strings.Contains(joined, "· Read article") {
		t.Fatalf("read row missing:\n%s", joined)
	}
}

func TestBlockRendererRedrawsInPlace(t *testing.T) {
	var buf bytes.Buffer
	r := &blockRenderer{out: &buf, interactive: true}
	r.Render([]string{"one", "two"})
	r.Render([]string{"three"})
	out := buf.String()
	if !strings.Contains(out, "\x1b[2A") {
		t.Fatalf("missing cursor-up: %q", out)
	}
	if c := strings.Count(out, "\x1b[2K"); c != 4 { // two on first render, two on the shrinking redraw
		t.Fatalf("clear count = %d: %q", c, out)
	}
	if !strings.Contains(out, "\x1b[1A") {
		t.Fatalf("missing final cursor adjustment: %q", out)
	}
}
