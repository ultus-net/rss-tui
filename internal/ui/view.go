package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/textutil"
	"github.com/ultus-net/rss-tui/internal/theme"
)

type loadedMsg struct {
	articles    []store.Article
	feeds       []store.Feed
	feedErrors  []feedError
	lastUpdated time.Time
	themeName   string
	err         error
}

type refreshDoneMsg struct {
	newCount int
	errors   []feedError
	at       time.Time
}

type beginRefreshMsg struct{}

type tickMsg time.Time

type spinnerTickMsg struct{}

type statusClearMsg struct{ seq int }

type browserDoneMsg struct{ err error }

type themeSavedMsg struct{ err error }

type yankDoneMsg struct {
	label string
	err   error
}

type actionErrMsg struct{ err error }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		if m.width < 20 {
			m.width = 20
		}
		if m.height < 6 {
			m.height = 6
		}
		m.clampCursor()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case loadedMsg:
		first := !m.ready
		m.ready = true
		if msg.err != nil {
			return m, m.flash("cache: " + msg.err.Error())
		}
		m.articles = msg.articles
		m.feeds = msg.feeds
		if first && len(msg.feedErrors) > 0 {
			m.feedErrors = msg.feedErrors
		}
		if !msg.lastUpdated.IsZero() {
			m.lastUpdated = msg.lastUpdated
		}
		if msg.themeName != "" && !m.deps.ThemeExplicit {
			if p, ok := theme.Resolve(msg.themeName); ok {
				m.theme = p
			}
		}
		m.recompute()
		return m, nil

	case beginRefreshMsg:
		return m, m.startRefresh()

	case tickMsg:
		if cmd := m.startRefresh(); cmd != nil {
			return m, cmd
		}
		// A manual refresh is already running; it reschedules on completion.
		return m, nil

	case spinnerTickMsg:
		if !m.refreshing {
			return m, nil
		}
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		return m, m.spinnerTick()

	case refreshDoneMsg:
		m.refreshing = false
		m.feedErrors = msg.errors
		if !msg.at.IsZero() {
			m.lastUpdated = msg.at
		}
		cmds := []tea.Cmd{m.loadCmd(), m.nextTick()}
		switch {
		case msg.newCount > 0 && len(msg.errors) > 0:
			cmds = append(cmds, m.flash(fmt.Sprintf("%d new · %d feeds failed", msg.newCount, len(msg.errors))))
		case msg.newCount > 0:
			cmds = append(cmds, m.flash(fmt.Sprintf("%d new articles", msg.newCount)))
		case len(msg.errors) > 0:
			cmds = append(cmds, m.flash(fmt.Sprintf("%d feeds failed — press e", len(msg.errors))))
		}
		return m, tea.Batch(cmds...)

	case statusClearMsg:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
		return m, nil

	case browserDoneMsg:
		if msg.err != nil {
			return m, m.flash("browser: " + msg.err.Error())
		}
		return m, nil

	case themeSavedMsg:
		if msg.err != nil {
			return m, m.flash("could not save theme: " + msg.err.Error())
		}
		return m, nil

	case yankDoneMsg:
		if msg.err != nil {
			return m, m.flash("clipboard: " + msg.err.Error())
		}
		return m, m.flash(msg.label)

	case actionErrMsg:
		if msg.err != nil {
			return m, m.flash(msg.err.Error())
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.overlay != overlayNone {
		m.overlay = overlayNone
		return m, nil
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.overlay = overlayHelp
	case "e":
		if len(m.feedErrors) == 0 {
			return m, m.flash("no feed errors")
		}
		m.overlay = overlayErrors
	case "F":
		m.overlay = overlayFeeds
	case "t":
		return m, m.cycleTheme()
	case "y":
		return m, m.yank(false)
	case "Y":
		return m, m.yank(true)
	case "tab":
		m.manualSingle = !m.manualSingle
		if m.singlePane() && m.selected() == nil {
			m.focus = focusList
		}
	case "u":
		m.unreadOnly = !m.unreadOnly
		m.cursor = 0
		m.offset = 0
		m.recompute()
		if m.unreadOnly {
			return m, m.flash("unread only")
		}
		return m, m.flash("all articles")
	case "c":
		m.cycleCategory()
		if m.category == "" {
			return m, m.flash("all categories")
		}
		return m, m.flash("category: " + m.category)
	case "/":
		m.searching = true
		m.status = ""
	case "r":
		if cmd := m.startRefresh(); cmd != nil {
			return m, cmd
		}
		return m, m.flash("refresh already running")
	case "m":
		ids := make([]int64, 0, len(m.visible))
		for _, a := range m.visible {
			if !a.Read {
				ids = append(ids, a.ID)
			}
		}
		if len(ids) == 0 {
			return m, m.flash("nothing unread here")
		}
		cmd := m.markRead(ids, true)
		return m, tea.Batch(cmd, m.flash(fmt.Sprintf("marked %d read", len(ids))))
	case "s":
		a := m.selected()
		if a == nil {
			return m, nil
		}
		starred := !a.Starred
		cmd := m.setStarred([]int64{a.ID}, starred)
		label := "unstarred"
		if starred {
			label = "starred"
		}
		return m, tea.Batch(cmd, m.flash(label))
	case "S":
		m.starredOnly = !m.starredOnly
		m.cursor = 0
		m.offset = 0
		m.recompute()
		if m.starredOnly {
			return m, m.flash("starred only")
		}
		return m, m.flash("all articles")
	case "o":
		return m, m.openInBrowser()
	case "enter":
		if m.focus == focusPreview {
			m.focus = focusList
			return m, nil
		}
		a := m.selected()
		if a == nil {
			return m, nil
		}
		m.focus = focusPreview
		m.previewScroll = 0
		if !a.Read {
			return m, m.markRead([]int64{a.ID}, true)
		}
		return m, nil
	case " ":
		a := m.selected()
		if a == nil {
			return m, nil
		}
		read := !a.Read
		cmd := m.markRead([]int64{a.ID}, read)
		label := "unread"
		if read {
			label = "read"
		}
		return m, tea.Batch(cmd, m.flash(label))
	case "esc":
		if m.focus == focusPreview {
			m.focus = focusList
			return m, nil
		}
		if m.query != "" {
			m.query = ""
			m.cursor = 0
			m.offset = 0
			m.recompute()
			return m, m.flash("filter cleared")
		}
	case "j", "down":
		m.moveFocus(1)
	case "k", "up":
		m.moveFocus(-1)
	case "n":
		if !m.nextUnread(1) {
			return m, m.flash("no unread below")
		}
	case "N":
		if !m.nextUnread(-1) {
			return m, m.flash("no unread above")
		}
	case "g", "home":
		m.jumpEdge(-1)
	case "G", "end":
		m.jumpEdge(1)
	case "ctrl+d", "pgdown":
		m.halfPage(1)
	case "ctrl+u", "pgup":
		m.halfPage(-1)
	case "h", "left":
		m.focus = focusList
	case "l", "right":
		if m.selected() != nil {
			m.focus = focusPreview
		}
	}
	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.query = ""
		m.cursor = 0
		m.offset = 0
		m.recompute()
		return m, m.flash("search cleared")
	case "enter":
		m.searching = false
		if m.query == "" {
			return m, nil
		}
		return m, m.flash("filter: " + m.query)
	case "backspace":
		runes := []rune(m.query)
		if len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
		}
		m.cursor = 0
		m.offset = 0
		m.recompute()
	case "ctrl+u":
		m.query = ""
		m.cursor = 0
		m.offset = 0
		m.recompute()
	case " ":
		m.query += " "
		m.cursor = 0
		m.offset = 0
		m.recompute()
	default:
		if msg.Type == tea.KeyRunes {
			m.query += string(msg.Runes)
			m.cursor = 0
			m.offset = 0
			m.recompute()
		}
	}
	return m, nil
}

func (m *Model) moveFocus(delta int) {
	if m.focus == focusPreview {
		m.scrollPreview(delta * 2)
		return
	}
	m.moveCursor(delta)
}

func (m *Model) jumpEdge(direction int) {
	if m.focus == focusPreview {
		if direction < 0 {
			m.previewScroll = 0
			return
		}
		m.scrollPreview(1 << 20)
		return
	}
	if len(m.visible) == 0 {
		return
	}
	if direction < 0 {
		m.cursor = 0
	} else {
		m.cursor = len(m.visible) - 1
	}
	m.clampOffset()
	m.previewScroll = 0
}

func (m *Model) halfPage(direction int) {
	if m.focus == focusPreview {
		step := m.previewBodyHeight() / 2
		if step < 1 {
			step = 1
		}
		m.scrollPreview(direction * step)
		return
	}
	step := m.listRows() / 2
	if step < 1 {
		step = 1
	}
	m.moveCursor(direction * step)
}

// View implements tea.Model.
func (m Model) View() string {
	st := newStyles(m.theme)
	if !m.ready {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, st.faint().Render("loading…"))
	}
	switch m.overlay {
	case overlayHelp:
		return m.viewHelp(st)
	case overlayErrors:
		return m.viewErrors(st)
	case overlayFeeds:
		return m.viewFeeds(st)
	}

	bodyHeight := m.paneHeight()
	var body string
	if m.singlePane() {
		if m.focus == focusPreview {
			body = m.renderPreview(m.width, bodyHeight, st, true)
		} else {
			body = m.renderList(m.width, bodyHeight, st, true)
		}
	} else {
		left := m.leftWidth()
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderList(left, bodyHeight, st, m.focus == focusList),
			m.renderPreview(m.width-left, bodyHeight, st, m.focus == focusPreview))
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderStatus(st))
}

func (m Model) listHeader() string {
	parts := []string{"All articles"}
	if m.category != "" {
		parts = []string{titleCase(m.category)}
	}
	if m.starredOnly {
		parts = append(parts, "starred")
	}
	if m.unreadOnly {
		parts = append(parts, "unread")
	}
	header := fmt.Sprintf("%s · %d", strings.Join(parts, " · "), len(m.visible))
	if m.query != "" {
		header += " · /" + m.query
	}
	return header
}

func (m Model) renderList(w, h int, st styles, active bool) string {
	contentW := w - 4
	if contentW < 8 {
		contentW = 8
	}
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}
	lines := make([]string, 0, innerH)
	lines = append(lines, st.header().Render(textutil.Truncate(m.listHeader(), contentW)))
	if innerH > 1 {
		lines = append(lines, "")
	}
	rows := innerH - len(lines)
	for i := 0; i < rows; i++ {
		idx := m.offset + i
		switch {
		case idx < len(m.visible):
			lines = append(lines, renderRow(m.visible[idx], contentW, idx == m.cursor, st))
		case i == 0 && len(m.visible) == 0:
			lines = append(lines, st.faint().Render(textutil.Truncate(m.emptyText(), contentW)))
		default:
			lines = append(lines, "")
		}
	}
	return st.pane(active).Width(w - 2).Height(innerH).Render(strings.Join(padLines(lines, innerH), "\n"))
}

func (m Model) emptyText() string {
	switch {
	case m.refreshing:
		return spinnerFrames[m.spinner] + " fetching feeds…"
	case m.query != "":
		return "no matches for /" + m.query
	case m.category != "":
		return "nothing in " + m.category
	case m.starredOnly && m.unreadOnly:
		return "no unread starred articles"
	case m.starredOnly:
		return "no starred articles"
	case m.unreadOnly:
		return "no unread articles"
	case len(m.articles) == 0:
		return "no articles yet · press r to refresh"
	default:
		return "nothing to show"
	}
}

// rowAgeMinWidth is the narrowest row that still shows the age stamp. Below it
// the feed tag and headline keep the space.
const rowAgeMinWidth = 60

func renderRow(a store.Article, width int, selected bool, st styles) string {
	if width <= 0 {
		return ""
	}
	marker := " "
	if selected {
		marker = "▍"
	}
	dot := "●"
	if a.Read {
		dot = "·"
	}
	tagW := width / 3
	if tagW < 6 {
		tagW = 6
	}
	if tagW > 16 {
		tagW = 16
	}
	if width-tagW < 12 {
		tagW = width - 12
	}
	if tagW < 0 {
		tagW = 0
	}
	tag := ""
	if tagW > 0 {
		tag = textutil.Truncate(a.FeedTitle, tagW)
	}
	age := ""
	if width >= rowAgeMinWidth {
		age = textutil.Age(a.Published, time.Now())
	}
	ageW := textutil.Width(age)
	titleW := width - 5 - textutil.Width(tag)
	if ageW > 0 {
		titleW -= ageW + 1
	}
	if titleW < 4 {
		titleW = 4
	}
	title := textutil.Truncate(a.Title, titleW)
	if a.Starred {
		title = textutil.Truncate("★ "+a.Title, titleW)
	}
	gap := width - 4 - textutil.Width(title) - textutil.Width(tag)
	if ageW > 0 {
		gap -= ageW + 1
	}
	if gap < 1 {
		gap = 1
	}
	sep := st.rowFill(selected).Render(" ")
	fill := st.rowFill(selected)
	line := st.rowMarker(selected).Render(marker) + sep +
		st.rowDot(a.Read, selected).Render(dot) + sep +
		st.rowTitle(a.Read, selected).Render(title) +
		fill.Render(strings.Repeat(" ", gap))
	if age != "" {
		line += st.rowAge(a.Read, selected).Render(age) + fill.Render(" ")
	}
	line += st.rowTag(selected).Render(tag)
	if used := ansi.StringWidth(line); used < width {
		line += fill.Render(strings.Repeat(" ", width-used))
	}
	return ansi.Truncate(line, width, "")
}

func (m Model) renderPreview(w, h int, st styles, active bool) string {
	contentW := w - 4
	if contentW < 8 {
		contentW = 8
	}
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}
	a := m.selected()
	if a == nil {
		return st.pane(active).Width(w - 2).Height(innerH).Render(strings.Join(padLines([]string{st.faint().Render("no article selected")}, innerH), "\n"))
	}
	m.ensurePreview()

	titleLines := textutil.Wrap(a.Title, contentW)
	if len(titleLines) > 3 {
		titleLines = titleLines[:3]
		titleLines[2] = textutil.Truncate(titleLines[2]+"…", contentW)
	}
	if len(titleLines) == 0 {
		titleLines = []string{"(untitled)"}
	}
	meta := a.FeedTitle
	if a.FeedCategory != "" {
		meta += " · " + a.FeedCategory
	}
	if stamp := textutil.RelativeTime(a.Published, time.Now()); stamp != "" {
		meta += " · " + stamp
	}
	if a.Author != "" {
		meta += " · " + a.Author
	}

	footer := "enter open · o browser"
	if m.focus == focusPreview {
		footer = "o open · esc back · j/k scroll"
	}
	bodyH := innerH - len(titleLines) - 1 - 1 - 1
	if bodyH < 1 {
		bodyH = 1
	}
	maxScroll := len(m.preview.lines) - bodyH
	if maxScroll > 0 && m.focus == focusPreview {
		footer = fmt.Sprintf("%s %d%%", footer, m.previewScroll*100/maxScroll)
	}

	lines := make([]string, 0, innerH)
	for _, t := range titleLines {
		lines = append(lines, st.title().Render(t))
	}
	lines = append(lines, st.meta().Render(textutil.Truncate(meta, contentW)))
	lines = append(lines, "")
	start := m.previewScroll
	end := start + bodyH
	if start > len(m.preview.lines) {
		start = len(m.preview.lines)
	}
	if end > len(m.preview.lines) {
		end = len(m.preview.lines)
	}
	for _, bodyLine := range m.preview.lines[start:end] {
		lines = append(lines, st.body().Render(bodyLine))
	}
	for len(lines) < innerH-1 {
		lines = append(lines, "")
	}
	lines = append(lines, st.faint().Render(textutil.Truncate(footer, contentW)))
	return st.pane(active).Width(w - 2).Height(innerH).Render(strings.Join(padLines(lines, innerH), "\n"))
}

func (m Model) renderStatus(st styles) string {
	inner := m.width - 2
	if inner < 1 {
		inner = 1
	}
	var left, right string
	if m.searching {
		left = st.statusText(m.theme.Search).Render("/" + m.query + "▏")
		right = st.statusText(m.theme.Faint).Render("enter apply · esc clear")
	} else {
		segs := []string{
			st.statusText(m.theme.Accent).Render("● " + strconv.Itoa(m.totalUnread()) + " unread"),
		}
		if m.unreadOnly {
			segs = append(segs, st.statusText(m.theme.Warning).Render("unread only"))
		}
		if m.starredOnly {
			segs = append(segs, st.statusText(m.theme.AccentAlt).Render("starred only"))
		}
		if m.category != "" {
			segs = append(segs, st.statusText(m.theme.AccentAlt).Render(m.category))
		}
		if q := strings.TrimSpace(m.query); q != "" {
			segs = append(segs, st.statusText(m.theme.Search).Render("/"+q))
		}
		if m.refreshing {
			segs = append(segs, st.statusText(m.theme.Accent).Render(spinnerFrames[m.spinner]+" refreshing"))
		} else if !m.lastUpdated.IsZero() {
			segs = append(segs, st.statusText(m.theme.Muted).Render("updated "+textutil.RelativeTime(m.lastUpdated, time.Now())))
		}
		if len(m.feedErrors) > 0 {
			segs = append(segs, st.statusText(m.theme.Error).Render(fmt.Sprintf("⚠ %d failed", len(m.feedErrors))))
		}
		if m.status != "" {
			segs = append(segs, st.statusText(m.theme.Warning).Render(m.status))
		}
		left = strings.Join(segs, st.statusText(m.theme.Faint).Render(" · "))

		pos := 0
		if len(m.visible) > 0 {
			pos = m.cursor + 1
		}
		right = strings.Join([]string{
			st.statusText(m.theme.Faint).Render(fmt.Sprintf("%d/%d", pos, len(m.visible))),
			st.statusText(m.theme.Faint).Render("? help"),
		}, st.statusText(m.theme.Faint).Render(" · "))
	}

	lw := ansi.StringWidth(left)
	rw := ansi.StringWidth(right)
	if lw+rw+1 > inner {
		keep := inner - rw - 1
		if keep < 0 {
			keep = 0
		}
		left = ansi.Truncate(left, keep, "…")
		lw = ansi.StringWidth(left)
	}
	pad := inner - lw - rw
	if pad < 1 {
		pad = 1
	}
	line := left + strings.Repeat(" ", pad) + right
	return st.statusBar().Render(ansi.Truncate(line, inner, "…"))
}

func (m Model) viewHelp(st styles) string {
	rows := [][2]string{
		{"j / ↓", "next article"},
		{"k / ↑", "previous article"},
		{"n / N", "next / previous unread"},
		{"g / G", "top / bottom"},
		{"ctrl+d/u", "half page"},
		{"h / l", "list / preview pane"},
		{"enter", "open preview (marks read)"},
		{"space", "toggle read / unread"},
		{"s", "star / unstar article"},
		{"S", "starred only"},
		{"esc", "back · clear filter"},
		{"o", "open in browser"},
		{"y / Y", "copy link / markdown link"},
		{"r", "refresh all feeds"},
		{"u", "all / unread only"},
		{"/", "fuzzy search headlines"},
		{"m", "mark visible as read"},
		{"c", "cycle category"},
		{"t", "cycle colour theme"},
		{"tab", "single-pane toggle"},
		{"F", "feed health"},
		{"e", "feed error details"},
		{"?", "close this help"},
		{"q", "quit"},
	}
	boxW := 66
	if m.width-6 < boxW {
		boxW = m.width - 6
	}
	if boxW < 32 {
		boxW = 32
	}
	keyW := 10
	textW := boxW - keyW - 4
	if textW < 12 {
		textW = 12
	}
	var b strings.Builder
	b.WriteString(st.title().Render("rss-tui"))
	b.WriteString("\n")
	b.WriteString(st.faint().Render("keys"))
	b.WriteString("\n\n")
	for _, row := range rows {
		b.WriteString(st.tag().Render(padRight(row[0], keyW)))
		b.WriteString(st.meta().Render(textutil.Truncate(row[1], textW)))
		b.WriteString("\n")
	}
	box := st.helpBox().Render(strings.TrimRight(b.String(), "\n"))
	box = fitHeight(box, m.height-1)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewErrors(st styles) string {
	width := 76
	if m.width-8 < width {
		width = m.width - 8
	}
	if width < 30 {
		width = 30
	}
	textW := width - 4
	var b strings.Builder
	b.WriteString(st.title().Render("feed errors"))
	b.WriteString("\n\n")
	if len(m.feedErrors) == 0 {
		b.WriteString(st.faint().Render("No feed errors recorded."))
	}
	for _, fe := range m.feedErrors {
		b.WriteString(st.tag().Render(textutil.Truncate(fe.Feed, textW)))
		b.WriteString("\n")
		for _, line := range textutil.Wrap(fe.Err, textW) {
			b.WriteString(st.failure().Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(st.faint().Render("any key to close"))
	box := st.helpBox().Render(strings.TrimRight(b.String(), "\n"))
	box = fitHeight(box, m.height-1)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewFeeds(st styles) string {
	width := 88
	if m.width-8 < width {
		width = m.width - 8
	}
	if width < 34 {
		width = 34
	}
	textW := width - 4
	var b strings.Builder
	b.WriteString(st.title().Render("feed health"))
	b.WriteString("\n\n")
	if len(m.feeds) == 0 {
		b.WriteString(st.faint().Render("No feeds cached yet. Press r to refresh."))
		b.WriteString("\n")
	}
	for _, f := range m.feeds {
		name := f.Title
		if strings.TrimSpace(name) == "" {
			name = f.URL
		}
		meta := fmt.Sprintf("%s · %d unread · %d cached · ", f.Category, f.Unread, f.Articles)
		if f.LastFetched.IsZero() {
			meta += "never fetched"
		} else {
			meta += "fetched " + textutil.RelativeTime(f.LastFetched, time.Now())
		}
		b.WriteString(st.tag().Render(textutil.Truncate(name, textW)))
		b.WriteString("\n")
		b.WriteString(st.meta().Render(textutil.Truncate(meta, textW)))
		b.WriteString("\n")
		if f.LastError != "" {
			for _, line := range textutil.Wrap(f.LastError, textW) {
				b.WriteString(st.failure().Render(line))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(st.faint().Render("any key to close"))
	box := st.helpBox().Render(strings.TrimRight(b.String(), "\n"))
	box = fitHeight(box, m.height-1)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func padLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func fitHeight(s string, height int) string {
	if height < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

func padRight(s string, width int) string {
	if gap := width - textutil.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	head := strings.ToUpper(string(runes[0]))
	return head + string(runes[1:])
}
