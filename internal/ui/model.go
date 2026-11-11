package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/feed"
	"github.com/ultus-net/rss-tui/internal/notify"
	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/textutil"
	"github.com/ultus-net/rss-tui/internal/theme"
)

const (
	narrowWidth  = 100
	statusTTL    = 5 * time.Second
	spinnerEvery = 120 * time.Millisecond
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type focusMode int

const (
	focusList focusMode = iota
	focusPreview
)

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayErrors
)

type feedError struct {
	Feed string
	Err  string
	At   time.Time
}

// Deps is everything the model needs, assembled in main.
type Deps struct {
	Cfg           *config.Config
	Store         *store.Store
	Fetcher       *feed.Fetcher
	Notifier      *notify.Notifier
	Theme         theme.Palette
	ThemeExplicit bool
	Browser       string
	ConfigPath    string
}

// Model is the Bubble Tea application state.
type Model struct {
	deps Deps

	articles   []store.Article
	visible    []store.Article
	categories []string

	cursor        int
	offset        int
	previewScroll int
	preview       *previewCache

	width  int
	height int
	ready  bool

	focus        focusMode
	manualSingle bool
	unreadOnly   bool
	category     string
	query        string
	searching    bool

	theme theme.Palette

	refreshing       bool
	spinner          int
	lastUpdated      time.Time
	lastRefreshStart time.Time

	status    string
	statusSeq int

	feedErrors []feedError
	overlay    overlay
}

type previewCache struct {
	articleID int64
	width     int
	lines     []string
}

// New builds the initial model.
func New(deps Deps) Model {
	if deps.Theme.Name == "" {
		deps.Theme = theme.Get(config.DefaultTheme)
	}
	m := Model{
		deps:       deps,
		width:      80,
		height:     24,
		theme:      deps.Theme,
		categories: deps.Cfg.Categories(),
		preview:    &previewCache{},
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadCmd(),
		func() tea.Msg { return beginRefreshMsg{} },
		m.nextTick(),
	)
}

// View and Update live in view.go.

func (m Model) nextTick() tea.Cmd {
	return tea.Tick(m.deps.Cfg.RefreshInterval.Std(), func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) spinnerTick() tea.Cmd {
	return tea.Tick(spinnerEvery, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

func (m *Model) startRefresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	m.spinner = 0
	m.lastRefreshStart = time.Now()
	return tea.Batch(m.refreshCmd(), m.spinnerTick())
}

func (m *Model) flash(text string) tea.Cmd {
	m.status = text
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(statusTTL, func(time.Time) tea.Msg { return statusClearMsg{seq: seq} })
}

func (m Model) listRows() int {
	rows := m.height - 1 - 2 - 2 // status, border, header + blank
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m Model) paneHeight() int {
	h := m.height - 1
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) wide() bool { return m.width >= narrowWidth }

func (m Model) singlePane() bool { return !m.wide() || m.manualSingle }

func (m Model) leftWidth() int {
	if m.singlePane() {
		return m.width
	}
	w := int(float64(m.width) * 0.38)
	if w < 34 {
		w = 34
	}
	if w > 62 {
		w = 62
	}
	if w > m.width-30 {
		w = m.width - 30
	}
	return w
}

func (m Model) previewWidth() int {
	if m.singlePane() {
		return m.width - 4
	}
	w := m.width - m.leftWidth() - 4
	if w < 10 {
		w = 10
	}
	return w
}

func (m Model) selected() *store.Article {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.visible[m.cursor]
}

func (m *Model) clampCursor() {
	if len(m.visible) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampOffset()
}

func (m *Model) clampOffset() {
	rows := m.listRows()
	maxOffset := len(m.visible) - rows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *Model) recompute() {
	var keepID int64
	if a := m.selected(); a != nil {
		keepID = a.ID
	}
	q := strings.TrimSpace(m.query)
	visible := m.visible[:0]
	for _, a := range m.articles {
		if m.unreadOnly && a.Read {
			continue
		}
		if m.category != "" && a.FeedCategory != m.category {
			continue
		}
		if q != "" && !fuzzyMatch(q, a.Title+" "+a.FeedTitle) {
			continue
		}
		visible = append(visible, a)
	}
	m.visible = visible
	if keepID != 0 {
		for i := range m.visible {
			if m.visible[i].ID == keepID {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
}

func (m Model) totalUnread() int {
	n := 0
	for _, a := range m.articles {
		if !a.Read {
			n++
		}
	}
	return n
}

func (m *Model) setRead(id int64, read bool) {
	for i := range m.articles {
		if m.articles[i].ID == id {
			m.articles[i].Read = read
		}
	}
	for i := range m.visible {
		if m.visible[i].ID == id {
			m.visible[i].Read = read
		}
	}
}

func (m *Model) markRead(ids []int64, read bool) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		m.setRead(id, read)
	}
	st := m.deps.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := st.MarkRead(ctx, ids, read); err != nil {
			return actionErrMsg{err: err}
		}
		return nil
	}
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
	m.previewScroll = 0
}

func (m *Model) ensurePreview() {
	a := m.selected()
	if a == nil {
		*m.preview = previewCache{}
		return
	}
	w := m.previewWidth()
	if m.preview.articleID == a.ID && m.preview.width == w {
		return
	}
	md := a.Content
	if strings.TrimSpace(md) == "" {
		md = a.Summary
	}
	*m.preview = previewCache{
		articleID: a.ID,
		width:     w,
		lines:     textutil.Wrap(textutil.PreviewText(md), w),
	}
}

func (m Model) previewBodyHeight() int {
	a := m.selected()
	if a == nil {
		return 1
	}
	w := m.previewWidth()
	titleLines := len(textutil.Wrap(a.Title, w))
	if titleLines < 1 {
		titleLines = 1
	}
	h := m.paneHeight() - 2 - titleLines - 1 - 1 - 1 // border, title, meta, gap, footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) scrollPreview(delta int) {
	m.ensurePreview()
	maxScroll := len(m.preview.lines) - m.previewBodyHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.previewScroll += delta
	if m.previewScroll < 0 {
		m.previewScroll = 0
	}
	if m.previewScroll > maxScroll {
		m.previewScroll = maxScroll
	}
}

func (m *Model) cycleCategory() {
	choices := append([]string{""}, m.categories...)
	next := 0
	for i, c := range choices {
		if c == m.category {
			next = (i + 1) % len(choices)
			break
		}
	}
	m.category = choices[next]
	m.cursor = 0
	m.offset = 0
	m.recompute()
}

func (m *Model) cycleTheme() tea.Cmd {
	next := theme.Next(m.theme.Name)
	m.theme = next
	st := m.deps.Store
	saved := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return themeSavedMsg{err: st.SetMeta(ctx, "theme", next.Name)}
	}
	flash := m.flash("theme: " + next.Label)
	return tea.Batch(flash, saved)
}

func (m *Model) openInBrowser() tea.Cmd {
	a := m.selected()
	if a == nil || strings.TrimSpace(a.Link) == "" {
		return m.flash("no link for this article")
	}
	cmd, err := browserCommand(m.deps.Browser, a.Link)
	if err != nil {
		return m.flash(err.Error())
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return browserDoneMsg{err: err} })
}

func browserCommand(configured, link string) (*exec.Cmd, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		switch runtime.GOOS {
		case "darwin":
			return exec.Command("open", link), nil
		case "windows":
			return exec.Command("rundll32", "url.dll,FileProtocolHandler", link), nil
		default:
			return exec.Command("xdg-open", link), nil
		}
	}
	fields := strings.Fields(configured)
	if len(fields) == 0 {
		return nil, fmt.Errorf("browser command is empty")
	}
	args := append(append([]string{}, fields[1:]...), link)
	return exec.Command(fields[0], args...), nil
}

func (m Model) loadCmd() tea.Cmd {
	st := m.deps.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		arts, err := st.Articles(ctx, store.ListOptions{})
		msg := loadedMsg{articles: arts, err: err}
		if err != nil {
			return msg
		}
		if v, ok, metaErr := st.Meta(ctx, "last_updated"); metaErr == nil && ok {
			if ts, perr := strconv.ParseInt(v, 10, 64); perr == nil {
				msg.lastUpdated = time.Unix(ts, 0)
			}
		}
		if v, ok, metaErr := st.Meta(ctx, "theme"); metaErr == nil && ok {
			msg.themeName = v
		}
		return msg
	}
}

func (m Model) refreshCmd() tea.Cmd {
	deps := m.deps
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		results := deps.Fetcher.FetchAll(ctx, deps.Cfg.Feeds)
		at := time.Now()
		msg := refreshDoneMsg{at: at}
		newFeeds := make([]string, 0, len(results))
		newTitles := make([]string, 0, len(results))
		for _, r := range results {
			if r.Err != nil {
				msg.errors = append(msg.errors, feedError{Feed: r.Title, Err: r.Err.Error(), At: at})
				_ = deps.Store.RecordFeedError(ctx, r.Feed.URL, r.Title, r.Feed.Category, r.Err.Error())
				continue
			}
			added, err := deps.Store.SyncFeed(ctx, toIngest(r))
			if err != nil {
				msg.errors = append(msg.errors, feedError{Feed: r.Title, Err: err.Error(), At: at})
				continue
			}
			if added > 0 {
				msg.newCount += added
				newFeeds = append(newFeeds, r.Title)
				if len(r.Items) > 0 {
					newTitles = append(newTitles, r.Items[0].Title)
				}
			}
		}
		if err := deps.Store.Prune(ctx, deps.Cfg.MaxArticlesPerFeed); err != nil {
			msg.errors = append(msg.errors, feedError{Feed: "cache", Err: err.Error(), At: at})
		}
		if err := deps.Store.SetMeta(ctx, "last_updated", strconv.FormatInt(at.Unix(), 10)); err != nil {
			msg.errors = append(msg.errors, feedError{Feed: "cache", Err: err.Error(), At: at})
		}
		if msg.newCount > 0 && deps.Notifier != nil && deps.Notifier.Enabled() {
			news := notify.NewArticles{Count: msg.newCount, Feeds: newFeeds, Titles: newTitles}
			go func() {
				_ = deps.Notifier.Send(context.Background(), news)
			}()
		}
		return msg
	}
}

func toIngest(r feed.Result) store.IngestFeed {
	out := store.IngestFeed{URL: r.Feed.URL, Title: r.Title, Category: r.Feed.Category}
	out.Articles = make([]store.IngestArticle, 0, len(r.Items))
	for _, it := range r.Items {
		out.Articles = append(out.Articles, store.IngestArticle{
			GUID:      it.GUID,
			Title:     it.Title,
			Link:      it.Link,
			Author:    it.Author,
			Summary:   it.Summary,
			Content:   it.Content,
			Published: it.Published,
		})
	}
	return out
}
