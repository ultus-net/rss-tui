package textutil

import (
	"strings"
	"testing"
	"time"
)

func TestCleanStripsEscapesAndControls(t *testing.T) {
	in := "red \x1b[31mtext\x1b[0m\x1b]0;title\x07\x01 ok\r\nline\ttab"
	got := Clean(in)
	if strings.ContainsAny(got, "\x1b\x01\r") {
		t.Fatalf("escape survived: %q", got)
	}
	if !strings.Contains(got, "red text") {
		t.Fatalf("content lost: %q", got)
	}
	if !strings.Contains(got, "line    tab") {
		t.Fatalf("tab not expanded: %q", got)
	}
}

func TestCleanCollapsesBlankRuns(t *testing.T) {
	if got := Clean("a\n\n\n\nb\n   \n"); got != "a\n\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateWide(t *testing.T) {
	got := Truncate("日本語テキスト", 5)
	if Width(got) > 5 {
		t.Fatalf("width %d for %q", Width(got), got)
	}
	if got == "" {
		t.Fatal("empty truncation")
	}
	if Truncate("abc", 0) != "" {
		t.Fatal("zero width should be empty")
	}
}

func TestWrapNeverExceedsWidth(t *testing.T) {
	lines := Wrap("alpha beta gamma supercalifragilisticexpialidocious delta\n\nsecond paragraph", 12)
	if len(lines) < 3 {
		t.Fatalf("lines = %v", lines)
	}
	for _, line := range lines {
		if Width(line) > 12 {
			t.Fatalf("line %q width %d", line, Width(line))
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "\n\n") {
		t.Fatalf("paragraph break lost: %q", joined)
	}
}

func TestMarkdownToText(t *testing.T) {
	md := "# Heading\n\nSome **bold** and *italic* and [link](https://example.com).\n" +
		"![alt text](image.png)\n\n- one\n- two\n\n```go\ncode *not italic*\n```\n\n" +
		"<https://angle.example> &amp; entity"
	got := MarkdownToText(md)
	for _, want := range []string{"Heading", "Some bold and italic and link.", "alt text", "• one", "code *not italic*", "https://angle.example", "& entity"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "```") || strings.Contains(got, "**") {
		t.Fatalf("markdown syntax survived:\n%s", got)
	}
}

func TestSummaryAndOneLine(t *testing.T) {
	got := Summary("<p>Hello <b>world</b></p>\n\nAgain", 20)
	if strings.Contains(got, "\n") || Width(got) > 20 {
		t.Fatalf("summary = %q", got)
	}
	if OneLine("  a\n\tb  ") != "a b" {
		t.Fatalf("OneLine = %q", OneLine("  a\n\tb  "))
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-48 * time.Hour), "2d ago"},
		{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "2 Jan"},
		{time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "2 Jan 2024"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := RelativeTime(c.at, now); got != c.want {
			t.Fatalf("RelativeTime(%s) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-48 * time.Hour), "2d"},
		{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "2 Jan"},
		{time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), "2 Jan 24"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := Age(c.at, now); got != c.want {
			t.Fatalf("Age(%s) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestClock(t *testing.T) {
	at := time.Date(2026, 11, 12, 9, 5, 0, 0, time.Local)
	if got := Clock(at); got != "09:05" {
		t.Fatalf("Clock = %q", got)
	}
}
