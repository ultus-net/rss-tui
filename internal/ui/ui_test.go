package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/feed"
	"github.com/ultus-net/rss-tui/internal/notify"
	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/textutil"
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

func TestStarredFilterComposes(t *testing.T) {
	m := testModel(120, 30)
	m.articles[0].Starred = true
	m.articles[2].Starred = true
	m.starredOnly = true
	m.recompute()
	if len(m.visible) != 2 {
		t.Fatalf("starred = %d", len(m.visible))
	}
	if header := m.listHeader(); !strings.Contains(header, "starred") {
		t.Fatalf("header does not name the filter: %q", header)
	}

	m.unreadOnly = true
	m.recompute()
	if len(m.visible) != 2 {
		t.Fatalf("starred+unread = %d rows", len(m.visible))
	}
	if !strings.Contains(m.listHeader(), "starred") || !strings.Contains(m.listHeader(), "unread") {
		t.Fatalf("header drops a composed filter: %q", m.listHeader())
	}

	m.category = "news"
	m.recompute()
	if len(m.visible) != 1 || m.visible[0].ID != 3 {
		t.Fatalf("starred+unread+news = %+v", m.visible)
	}

	m.query = "tooling"
	m.recompute()
	if len(m.visible) != 1 || m.visible[0].ID != 3 {
		t.Fatalf("starred+unread+news+/tooling = %+v", m.visible)
	}

	m.query = "zzz"
	m.recompute()
	if len(m.visible) != 0 {
		t.Fatalf("non-matching query kept %d rows", len(m.visible))
	}
	if text := m.emptyText(); text != "no matches for /zzz" {
		t.Fatalf("query empty text = %q", text)
	}

	m.query = ""
	m.category = ""
	if text := m.emptyText(); text != "no unread starred articles" {
		t.Fatalf("empty text = %q", text)
	}
	m.unreadOnly = false
	if text := m.emptyText(); text != "no starred articles" {
		t.Fatalf("starred empty text = %q", text)
	}
}

func TestRowShowsAge(t *testing.T) {
	st := newStyles(theme.Get("nord"))
	a := store.Article{
		Title: "Headline", FeedTitle: "Example", Published: time.Now().Add(-3 * time.Hour),
	}
	for _, width := range []int{60, 80, 120} {
		line := renderRow(a, width, false, st)
		if !strings.Contains(line, "3h") {
			t.Fatalf("width %d: age missing from %q", width, line)
		}
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("width %d: rendered %d columns: %q", width, got, line)
		}
	}
	for _, width := range []int{20, 40, 59} {
		line := renderRow(a, width, false, st)
		if strings.Contains(line, "3h") {
			t.Fatalf("width %d: age crowds a narrow row: %q", width, line)
		}
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("width %d: rendered %d columns: %q", width, got, line)
		}
	}

	// At the cutoff the age must not displace the truncated feed tag.
	long := a
	long.FeedTitle = "A very long feed title that must be truncated"
	line := ansi.Strip(renderRow(long, rowAgeMinWidth, false, st))
	tag := textutil.Truncate(long.FeedTitle, 16)
	if !strings.Contains(line, "3h") || !strings.Contains(line, tag) {
		t.Fatalf("cutoff row lost the age or the tag %q: %q", tag, line)
	}

	// Every width above the cutoff composes to exactly the pane width, even
	// with a long headline and a long feed title.
	long.Title = strings.Repeat("Headline ", 20)
	for width := rowAgeMinWidth; width <= 200; width++ {
		line := renderRow(long, width, false, st)
		if !strings.Contains(line, "3h") {
			t.Fatalf("width %d: age missing", width)
		}
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("width %d: rendered %d columns", width, got)
		}
	}
}

func TestStarredRowShowsMarker(t *testing.T) {
	st := newStyles(theme.Get("nord"))
	a := store.Article{Title: "Kept article", FeedTitle: "Example", Starred: true}
	line := renderRow(a, 60, false, st)
	if !strings.Contains(line, "★") || !strings.Contains(line, "Kept article") {
		t.Fatalf("starred row = %q", line)
	}
	plain := renderRow(store.Article{Title: "Kept article", FeedTitle: "Example"}, 60, false, st)
	if strings.Contains(plain, "★") {
		t.Fatalf("unstarred row has a marker: %q", plain)
	}
	for _, width := range []int{20, 40} {
		narrow := renderRow(store.Article{Title: strings.Repeat("Headline ", 6), FeedTitle: "Example", Starred: true}, width, false, st)
		if got := ansi.StringWidth(narrow); got > width {
			t.Fatalf("starred row at width %d rendered %d columns: %q", width, got, narrow)
		}
	}
}

func TestStarKeyPersistsStar(t *testing.T) {
	oldTTL := statusTTL
	statusTTL = time.Millisecond
	defer func() { statusTTL = oldTTL }()

	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	if _, err := st.SyncFeed(ctx, store.IngestFeed{
		URL: "https://example.com/rss", Title: "Example", Category: "tech",
		Articles: []store.IngestArticle{{
			GUID: "g1", Title: "Go 1.27", Link: "https://example.com/go", Published: time.Now(),
		}},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	arts, err := st.Articles(ctx, store.ListOptions{})
	if err != nil || len(arts) != 1 {
		t.Fatalf("articles = %d, err %v", len(arts), err)
	}

	cfg := &config.Config{
		RefreshInterval: config.Duration(10 * time.Minute),
		Feeds:           []config.Feed{{URL: "https://example.com/rss", Title: "Example", Category: "tech"}},
	}
	m := New(Deps{Cfg: cfg, Store: st, Theme: theme.Get("nord")})
	m.width, m.height, m.ready = 120, 30, true
	m.articles = arts
	m.recompute()

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = model.(Model)
	runCmds(cmd)
	if !m.selected().Starred {
		t.Fatal("the key press did not star the row in memory")
	}
	stored, _ := st.Articles(ctx, store.ListOptions{StarredOnly: true})
	if len(stored) != 1 || stored[0].ID != arts[0].ID {
		t.Fatalf("star not persisted: %+v", stored)
	}

	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = model.(Model)
	runCmds(cmd)
	if m.selected().Starred {
		t.Fatal("the second key press did not unstar the row")
	}
	stored, _ = st.Articles(ctx, store.ListOptions{StarredOnly: true})
	if len(stored) != 0 {
		t.Fatalf("unstar not persisted: %+v", stored)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = model.(Model)
	if !m.starredOnly || m.status != "starred only" {
		t.Fatalf("S did not enable the filter: starred=%v status=%q", m.starredOnly, m.status)
	}
	if len(m.visible) != 0 {
		t.Fatalf("starred filter kept %d unstarred rows", len(m.visible))
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = model.(Model)
	if m.starredOnly || len(m.visible) != 1 {
		t.Fatalf("S did not clear the filter: starred=%v visible=%d", m.starredOnly, len(m.visible))
	}
}

// runCmds executes a command and every command it batches. The test shortens
// statusTTL, so the flash tick returns instead of sleeping out the real TTL.
func runCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return
	}
	for _, c := range batch {
		if c != nil {
			c()
		}
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

func TestNextUnread(t *testing.T) {
	m := testModel(120, 30)
	m.cursor = 0
	if !m.nextUnread(1) || m.cursor != 2 {
		t.Fatalf("forward from 0: cursor = %d", m.cursor)
	}
	if m.nextUnread(1) {
		t.Fatal("found an unread past the last row")
	}
	if !m.nextUnread(-1) || m.cursor != 0 {
		t.Fatalf("backward: cursor = %d", m.cursor)
	}
	if m.nextUnread(-1) {
		t.Fatal("found an unread above the first row")
	}

	m.cursor = 1
	m.visible[1].Read = true
	if !m.nextUnread(1) || m.cursor != 2 {
		t.Fatalf("skipped a read row incorrectly: %d", m.cursor)
	}
	if !m.nextUnread(-1) || m.cursor != 0 {
		t.Fatalf("backward skipped an unread row: %d", m.cursor)
	}
}

func TestCopySequenceEncodesClipboardPayload(t *testing.T) {
	seq := copySequence("https://example.com/go")
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("sequence shape = %q", seq)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if string(decoded) != "https://example.com/go" {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestYankWithoutLinkFlashes(t *testing.T) {
	m := testModel(120, 30)
	m.articles[0].Link = ""
	m.visible[0].Link = ""
	m.cursor = 0
	m.yank(false)
	if m.status != "no link to copy" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestFeedHealthOverlay(t *testing.T) {
	m := testModel(120, 30)
	m.feeds = []store.Feed{
		{URL: "https://a.example/rss", Title: "Alpha", Category: "tech", LastFetched: time.Now().Add(-3 * time.Hour), Unread: 3, Articles: 10},
		{URL: "https://b.example/rss", Title: "Broken", Category: "news", LastError: "HTTP 500 nope"},
	}
	m.overlay = overlayFeeds
	view := m.View()
	for _, want := range []string{"feed health", "Alpha", "3 unread", "10 cached", "fetched 3h ago", "HTTP 500 nope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("overlay lines = %d, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > m.width {
			t.Fatalf("overlay line %d width %d > %d", i, w, m.width)
		}
	}
}

func TestLoadedMsgSeedsPersistedErrorsOnce(t *testing.T) {
	m := testModel(120, 30)
	m.ready = false
	persisted := feedError{Feed: "Broken", Err: "HTTP 500", At: time.Now()}

	model, _ := m.Update(loadedMsg{articles: m.articles, feedErrors: []feedError{persisted}})
	m = model.(Model)
	if len(m.feedErrors) != 1 || m.feedErrors[0].Err != "HTTP 500" {
		t.Fatalf("persisted error not seeded: %+v", m.feedErrors)
	}

	model, _ = m.Update(loadedMsg{articles: m.articles})
	m = model.(Model)
	if len(m.feedErrors) != 1 {
		t.Fatalf("a later load wiped live errors: %+v", m.feedErrors)
	}
}

func TestRefreshCmdRevalidatesAndClearsErrors(t *testing.T) {
	const etag = `"v1"`
	const body = `<?xml version="1.0"?><rss version="2.0"><channel><title>Local</title>
	  <item><title>One</title><guid>one</guid><description>first</description></item>
	  <item><title>Two</title><guid>two</guid><description>second</description></item>
	</channel></rss>`
	conditional := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			conditional++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{
		RefreshInterval: config.Duration(10 * time.Minute),
		Feeds:           []config.Feed{{URL: srv.URL, Title: "Local", Category: "tech"}},
	}
	m := New(Deps{
		Cfg:     cfg,
		Store:   st,
		Fetcher: feed.New(feed.Options{Timeout: 2 * time.Second, Workers: 1}),
		Theme:   theme.Get("nord"),
	})

	first := m.refreshCmd()().(refreshDoneMsg)
	if first.newCount != 2 || len(first.errors) != 0 {
		t.Fatalf("first refresh = %+v", first)
	}

	if err := st.RecordFeedError(ctx, srv.URL, "Local", "tech", "stale error"); err != nil {
		t.Fatalf("record error: %v", err)
	}
	loaded := m.loadCmd()().(loadedMsg)
	if len(loaded.feeds) != 1 || loaded.feeds[0].ETag != etag {
		t.Fatalf("validators not persisted: %+v", loaded.feeds)
	}
	if len(loaded.feedErrors) != 1 || !strings.Contains(loaded.feedErrors[0].Err, "stale error") {
		t.Fatalf("persisted error missing from loadCmd: %+v", loaded.feedErrors)
	}

	second := m.refreshCmd()().(refreshDoneMsg)
	if second.newCount != 0 || len(second.errors) != 0 {
		t.Fatalf("second refresh = %+v", second)
	}
	if conditional != 1 {
		t.Fatalf("conditional requests = %d, want 1", conditional)
	}
	feeds, err := st.Feeds(ctx)
	if err != nil {
		t.Fatalf("feeds: %v", err)
	}
	if feeds[0].LastError != "" {
		t.Fatalf("304 did not clear the stored error: %q", feeds[0].LastError)
	}
	if feeds[0].Articles != 2 {
		t.Fatalf("304 changed the cache: %d articles", feeds[0].Articles)
	}
}

// TestRefreshDoneReplacesErrorList pins the intended semantics: each cycle
// polls every configured feed, so its result set is the complete health
// picture and a stale persisted error must not survive a healthy poll.
func TestRefreshDoneReplacesErrorList(t *testing.T) {
	m := testModel(120, 30)
	m.feedErrors = []feedError{{Feed: "Old", Err: "stale", At: time.Now()}}

	model, _ := m.Update(refreshDoneMsg{at: time.Now()})
	m = model.(Model)
	if len(m.feedErrors) != 0 {
		t.Fatalf("a healthy poll kept stale errors: %+v", m.feedErrors)
	}

	model, _ = m.Update(refreshDoneMsg{at: time.Now(), errors: []feedError{{Feed: "New", Err: "HTTP 500"}}})
	m = model.(Model)
	if len(m.feedErrors) != 1 || m.feedErrors[0].Feed != "New" {
		t.Fatalf("a failed feed was not reported: %+v", m.feedErrors)
	}
}

func TestPassiveReservesRowsForFailures(t *testing.T) {
	now := time.Now()
	articles := make([]store.Article, 0, 20)
	for i := 0; i < 20; i++ {
		articles = append(articles, store.Article{
			Title: fmt.Sprintf("Headline %d", i), FeedTitle: "Example", Published: now,
		})
	}
	v := passiveView{
		Now: now, Updated: now, Unread: 20, FeedCount: 4,
		Failures: []feedError{
			{Feed: "Alpha", Err: "timed out after 20s"},
			{Feed: "Beta", Err: "HTTP 500"},
			{Feed: "Gamma", Err: "parse feed"},
		},
		Articles: articles,
		Refresh:  10 * time.Minute,
		Width:    100,
		Height:   8,
		Palette:  theme.Get("nord"),
	}
	lines := buildPassiveLines(v)
	if len(lines) > v.Height-1 {
		t.Fatalf("lines = %d, height cap %d", len(lines), v.Height-1)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Alpha: timed out") {
		t.Fatalf("failure rows were trimmed away:\n%s", joined)
	}
	if !strings.Contains(joined, "1 more failed") {
		t.Fatalf("overflow row missing:\n%s", joined)
	}
	if !strings.Contains(joined, "Headline 0") {
		t.Fatalf("no article row survived:\n%s", joined)
	}
	if strings.Contains(joined, "Headline 19") {
		t.Fatalf("articles should yield height to failures:\n%s", joined)
	}
}

func keyPress(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	model, cmd := m.Update(msg)
	return model.(Model), cmd
}

func TestSettingsMenuChangesAndPersists(t *testing.T) {
	oldTTL := statusTTL
	statusTTL = time.Millisecond
	defer func() { statusTTL = oldTTL }()

	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := `# my settings
refresh_interval: 10m
theme: nord
notifications: false

# subscriptions
feeds:
  - url: https://example.com/rss
    title: Example
    category: tech
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	notifier := notify.New(false)
	m := New(Deps{Cfg: cfg, ConfigPath: path, Notifier: notifier, Theme: theme.Get(cfg.Theme)})
	m.width, m.height, m.ready = 100, 30, true

	m, cmd := keyPress(t, m, ",")
	runCmds(cmd)
	if m.overlay != overlaySettings {
		t.Fatalf(", did not open settings: overlay=%v", m.overlay)
	}

	m, cmd = keyPress(t, m, "l")
	runCmds(cmd)
	if m.theme.Name != "gruvbox-dark" || cfg.Theme != "gruvbox-dark" {
		t.Fatalf("theme after l = %q / cfg %q", m.theme.Name, cfg.Theme)
	}

	m, _ = keyPress(t, m, "j")
	m, cmd = keyPress(t, m, "l")
	runCmds(cmd)
	if cfg.RefreshInterval.Std() != 15*time.Minute {
		t.Fatalf("refresh = %s", cfg.RefreshInterval)
	}

	m, _ = keyPress(t, m, "j")
	m, cmd = keyPress(t, m, "l")
	runCmds(cmd)
	if !cfg.Notifications || !notifier.Enabled() {
		t.Fatalf("notifications cfg=%v notifier=%v", cfg.Notifications, notifier.Enabled())
	}

	m, _ = keyPress(t, m, "j")
	m, cmd = keyPress(t, m, "l")
	runCmds(cmd)
	if cfg.PassiveItems != 30 {
		t.Fatalf("passive items = %d", cfg.PassiveItems)
	}

	m, _ = keyPress(t, m, "j")
	m, cmd = keyPress(t, m, "l")
	runCmds(cmd)
	if !cfg.PassiveUnreadOnly {
		t.Fatal("passive unread only did not toggle")
	}

	m, _ = keyPress(t, m, "g")
	m, cmd = keyPress(t, m, "h")
	runCmds(cmd)
	if cfg.Theme != "nord" || m.theme.Name != "nord" {
		t.Fatalf("theme after h = %q / cfg %q", m.theme.Name, cfg.Theme)
	}

	m, _ = keyPress(t, m, "esc")
	if m.overlay != overlayNone {
		t.Fatalf("esc did not close settings: overlay=%v", m.overlay)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	for _, want := range []string{"# my settings", "# subscriptions", "refresh_interval: 15m", "theme: nord"} {
		if !strings.Contains(string(saved), want) {
			t.Fatalf("saved config missing %q:\n%s", want, saved)
		}
	}

	reloaded, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Theme != "nord" || reloaded.RefreshInterval.Std() != 15*time.Minute {
		t.Fatalf("reloaded theme/refresh = %q/%s", reloaded.Theme, reloaded.RefreshInterval)
	}
	if !reloaded.Notifications || reloaded.PassiveItems != 30 || !reloaded.PassiveUnreadOnly {
		t.Fatalf("reloaded flags = %v/%d/%v", reloaded.Notifications, reloaded.PassiveItems, reloaded.PassiveUnreadOnly)
	}
	if len(reloaded.Feeds) != 1 || reloaded.Feeds[0].Title != "Example" {
		t.Fatalf("feeds changed: %+v", reloaded.Feeds)
	}
}

func TestSettingsMenuRendersAndFits(t *testing.T) {
	cfg := &config.Config{RefreshInterval: config.Duration(10 * time.Minute), PassiveItems: 20}
	cfg.Theme = "nord"
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 12}} {
		m := New(Deps{Cfg: cfg, ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), Theme: theme.Get("nord")})
		m.width, m.height, m.ready = size[0], size[1], true
		m.overlay = overlaySettings
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Fatalf("%dx%d: view lines = %d", size[0], size[1], len(lines))
		}
		if !strings.Contains(m.View(), "settings") {
			t.Fatalf("%dx%d: settings title missing", size[0], size[1])
		}
	}
}

func TestSettingsSaveErrorFlashes(t *testing.T) {
	cfg := &config.Config{RefreshInterval: config.Duration(10 * time.Minute)}
	cfg.Theme = "nord"
	m := New(Deps{Cfg: cfg, Theme: theme.Get("nord")})
	m.width, m.height, m.ready = 100, 30, true
	m.overlay = overlaySettings
	m.settingsCursor = settingNotifications

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = model.(Model)
	if cmd == nil {
		t.Fatal("change produced no command")
	}
	if !strings.Contains(m.status, "notifications") {
		t.Fatalf("status = %q", m.status)
	}
	model, _ = m.Update(settingsSavedMsg{err: fmt.Errorf("config: empty path")})
	m = model.(Model)
	if !strings.Contains(m.status, "could not save settings") {
		t.Fatalf("save error did not flash: %q", m.status)
	}
}
