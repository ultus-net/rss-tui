package opml

import (
	"bytes"
	"strings"
	"testing"
)

func TestExportGroupsByCategoryAndRoundTrips(t *testing.T) {
	feeds := []Feed{
		{Title: "A & B", XMLURL: "https://a.example/rss", Category: "tech"},
		{Title: "B", XMLURL: "https://b.example/rss", Category: "news"},
		{Title: "C", XMLURL: "https://c.example/rss", Category: "tech"},
		{Title: "D", XMLURL: "https://d.example/rss"},
	}
	var buf bytes.Buffer
	if err := Export(&buf, feeds); err != nil {
		t.Fatalf("Export: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("missing XML header:\n%s", out)
	}
	// Two category outlines plus four feed outlines.
	if got := strings.Count(out, "<outline"); got != 6 {
		t.Fatalf("outlines = %d, want 6:\n%s", got, out)
	}
	if !strings.Contains(out, "A &amp; B") {
		t.Fatalf("title not escaped:\n%s", out)
	}

	parsed, err := Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed) != len(feeds) {
		t.Fatalf("parsed %d feeds, want %d", len(parsed), len(feeds))
	}
	byURL := make(map[string]Feed, len(parsed))
	for _, f := range parsed {
		byURL[f.XMLURL] = f
	}
	if byURL["https://a.example/rss"].Category != "tech" || byURL["https://a.example/rss"].Title != "A & B" {
		t.Fatalf("grouped feed = %+v", byURL["https://a.example/rss"])
	}
	if byURL["https://b.example/rss"].Category != "news" {
		t.Fatalf("news feed = %+v", byURL["https://b.example/rss"])
	}
	if byURL["https://d.example/rss"].Category != "" {
		t.Fatalf("uncategorised feed = %+v", byURL["https://d.example/rss"])
	}
}

func TestParseNestedCategoriesAndDuplicates(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>subs</title></head>
  <body>
    <outline text="Tech">
      <outline text="One" type="rss" xmlUrl="https://one.example/rss"/>
      <outline text="Two" type="rss" xmlUrl="https://two.example/rss" htmlUrl="https://two.example/"/>
    </outline>
    <outline title="News" type="rss" xmlurl="https://news.example/rss" htmlurl="https://news.example/"/>
    <outline text="Group without feeds"/>
    <outline text="Duplicate" xmlUrl="https://one.example/rss"/>
  </body>
</opml>`
	feeds, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feeds) != 3 {
		t.Fatalf("feeds = %d, want 3: %+v", len(feeds), feeds)
	}
	if feeds[0].Title != "One" || feeds[0].Category != "Tech" {
		t.Fatalf("nested feed = %+v", feeds[0])
	}
	if feeds[1].HTMLURL != "https://two.example/" {
		t.Fatalf("html url = %q", feeds[1].HTMLURL)
	}
	if feeds[2].XMLURL != "https://news.example/rss" || feeds[2].HTMLURL != "https://news.example/" {
		t.Fatalf("lowercase attributes ignored: %+v", feeds[2])
	}
	if feeds[2].Category != "" {
		t.Fatalf("flat feed category = %q", feeds[2].Category)
	}
}

func TestParseRejectsInvalidXML(t *testing.T) {
	if _, err := Parse(strings.NewReader("<opml><body>")); err == nil {
		t.Fatal("expected a parse error for truncated XML")
	}
}

func TestExportEmptyWritesValidDocument(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, nil); err != nil {
		t.Fatalf("Export: %v", err)
	}
	feeds, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feeds) != 0 {
		t.Fatalf("feeds = %d, want 0", len(feeds))
	}
}
