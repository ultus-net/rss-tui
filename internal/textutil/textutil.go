// Package textutil cleans untrusted feed text and shapes it for a terminal.
package textutil

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

var (
	ansiCSI   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSC   = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	ansiOther = regexp.MustCompile(`\x1b[@-_]`)
	control   = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	blankRuns = regexp.MustCompile(`\n{3,}`)

	imageRE   = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	linkRE    = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	angleRE   = regexp.MustCompile(`<(https?://[^>\s]+)>`)
	boldRE    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	boldAltRE = regexp.MustCompile(`__([^_]+)__`)
	italRE    = regexp.MustCompile(`\*([^*]+)\*`)
	italAltRE = regexp.MustCompile(`_([^_]+)_`)
	strikeRE  = regexp.MustCompile(`~~([^~]+)~~`)
	codeRE    = regexp.MustCompile("`([^`]+)`")
	escapeRE  = regexp.MustCompile(`\\([\\` + "`" + `*_{}\[\]()#+\-.!>])`)
	tagRE     = regexp.MustCompile(`<[^>]+>`)

	headingRE = regexp.MustCompile(`^#{1,6}\s+`)
	quoteRE   = regexp.MustCompile(`^\s*(?:>\s?)+`)
	bulletRE  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	ruleRE    = regexp.MustCompile(`^\s*(?:[-*_]\s*){3,}$`)
	tableSEP  = regexp.MustCompile(`^\s*\|?[\s:|-]+\|?\s*$`)
)

// Clean removes terminal escape sequences and control characters from feed
// text, then normalises whitespace. Feed content is untrusted input, so every
// string that reaches the renderer goes through here first.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiOther.ReplaceAllString(s, "")
	s = control.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\t", "    ")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	s = strings.Join(lines, "\n")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// OneLine collapses all whitespace runs into single spaces.
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Truncate shortens s to width terminal cells, appending an ellipsis.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return runewidth.Truncate(s, width, "…")
}

// Width is the display width of s in terminal cells.
func Width(s string) int { return runewidth.StringWidth(s) }

// Wrap word-wraps s to width cells, preserving paragraph breaks and
// hard-splitting words that are wider than the line.
func Wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	var out []string
	flush := func(line *string) {
		if *line != "" {
			out = append(out, *line)
			*line = ""
		}
	}
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			for runewidth.StringWidth(word) > width {
				flush(&line)
				head, rest := splitWide(word, width)
				if head == "" {
					break
				}
				out = append(out, head)
				word = rest
			}
			if word == "" {
				continue
			}
			switch {
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= width:
				line += " " + word
			default:
				flush(&line)
				line = word
			}
		}
		flush(&line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func splitWide(word string, width int) (head, rest string) {
	used := 0
	for i, r := range word {
		rw := runewidth.RuneWidth(r)
		if used+rw > width {
			return word[:i], word[i:]
		}
		used += rw
	}
	return word, ""
}

// MarkdownToText strips markdown syntax so the preview pane reads as prose.
func MarkdownToText(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	var b strings.Builder
	inFence := false
	for _, raw := range strings.Split(md, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			b.WriteByte('\n')
			continue
		}
		if ruleRE.MatchString(trimmed) {
			b.WriteString("────\n")
			continue
		}
		line = headingRE.ReplaceAllString(line, "")
		line = quoteRE.ReplaceAllString(line, "")
		if m := bulletRE.FindStringSubmatch(line); m != nil {
			line = m[1] + "• " + line[len(m[0]):]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "|") && tableSEP.MatchString(line) {
			continue
		}
		line = inlineToText(line)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return Clean(b.String())
}

func inlineToText(s string) string {
	s = imageRE.ReplaceAllString(s, "$1")
	s = linkRE.ReplaceAllString(s, "$1")
	s = angleRE.ReplaceAllString(s, "$1")
	s = boldRE.ReplaceAllString(s, "$1")
	s = boldAltRE.ReplaceAllString(s, "$1")
	s = italRE.ReplaceAllString(s, "$1")
	s = italAltRE.ReplaceAllString(s, "$1")
	s = strikeRE.ReplaceAllString(s, "$1")
	s = codeRE.ReplaceAllString(s, "$1")
	s = escapeRE.ReplaceAllString(s, "$1")
	s = tagRE.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// PreviewText renders article markdown as plain text.
func PreviewText(md string) string { return MarkdownToText(Clean(md)) }

// Summary is a one-line excerpt of markdown, capped at max cells.
func Summary(md string, max int) string {
	return Truncate(OneLine(MarkdownToText(md)), max)
}

// Age is the compact age stamp used by the list row. It shares the thresholds
// in ageBucket with RelativeTime and drops the " ago" suffix, because a row has
// fewer columns to spare. A date falls back to "2 Jan 06" past this year.
func Age(t, now time.Time) string {
	d, ok := elapsed(t, now)
	if !ok {
		return ""
	}
	if n, unit, ok := ageBucket(d); ok {
		if unit == 's' {
			return "now"
		}
		return fmt.Sprintf("%d%c", n, unit)
	}
	if t.Year() == now.Year() {
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 06")
}

// RelativeTime formats t relative to now for status bars and lists.
func RelativeTime(t, now time.Time) string {
	d, ok := elapsed(t, now)
	if !ok {
		return ""
	}
	if n, unit, ok := ageBucket(d); ok {
		if unit == 's' {
			return "just now"
		}
		return fmt.Sprintf("%d%c ago", n, unit)
	}
	if t.Year() == now.Year() {
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// elapsed clamps a stamp into a non-negative distance from now. ok is false for
// a zero stamp, which has no age.
func elapsed(t, now time.Time) (time.Duration, bool) {
	if t.IsZero() {
		return 0, false
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	return d, true
}

// ageBucket classifies a distance as a compact count and unit. ok is false when
// the stamp is old enough to print as a calendar date instead.
func ageBucket(d time.Duration) (n int, unit byte, ok bool) {
	switch {
	case d < 45*time.Second:
		return 0, 's', true
	case d < 90*time.Minute:
		return int(d.Minutes()), 'm', true
	case d < 36*time.Hour:
		return int(d.Hours()), 'h', true
	case d < 7*24*time.Hour:
		return int(d.Hours() / 24), 'd', true
	}
	return 0, 0, false
}

// Clock is the hh:mm stamp used by passive mode.
func Clock(t time.Time) string { return t.Local().Format("15:04") }
