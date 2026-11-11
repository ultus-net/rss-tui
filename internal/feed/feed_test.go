package feed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/ultus-net/rss-tui/internal/config"
)

const rssBody = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Parsed Feed</title>
    <link>https://example.com/blog/</link>
    <item>
      <title>First &amp; One</title>
      <link>/first</link>
      <guid>guid-1</guid>
      <description><![CDATA[<p>Hello <strong>world</strong></p><script>alert(1)</script>]]></description>
      <pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate>
    </item>
    <item>
      <title>Second</title>
      <link>https://elsewhere.example/second</link>
      <guid>guid-2</guid>
      <description><![CDATA[<p>Body two</p>]]></description>
    </item>
    <item>
      <title>Duplicate</title>
      <link>/dup</link>
      <guid>guid-1</guid>
      <description>ignored</description>
    </item>
  </channel>
</rss>`

const atomBody = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Feed</title>
  <link href="https://atom.example/"/>
  <entry>
    <title>A1</title>
    <link href="/a1"/>
    <id>tag:atom.example,2024:a1</id>
    <updated>2024-05-01T10:00:00Z</updated>
    <content type="html">&lt;p&gt;Atom &lt;em&gt;body&lt;/em&gt;&lt;/p&gt;</content>
  </entry>
</feed>`

func newFetcher(timeout time.Duration) *Fetcher {
	return New(Options{Timeout: timeout, Workers: 3})
}

func TestFetchRSSAndConvertHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssBody)
	}))
	defer srv.Close()

	results := newFetcher(2*time.Second).FetchAll(context.Background(), []config.Feed{{
		URL: srv.URL + "/rss", Title: "Custom", Category: "tech",
	}}, nil)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("fetch: %v", r.Err)
	}
	if r.Title != "Custom" {
		t.Fatalf("config title should win, got %q", r.Title)
	}
	if len(r.Items) != 2 {
		t.Fatalf("items = %d, want 2 after dedupe", len(r.Items))
	}
	first := r.Items[0]
	if first.Title != "First & One" {
		t.Fatalf("title = %q", first.Title)
	}
	if first.Link != "https://example.com/first" {
		t.Fatalf("relative link = %q", first.Link)
	}
	if !first.Published.Equal(time.Date(2023, 1, 2, 15, 4, 5, 0, time.UTC)) {
		t.Fatalf("published = %s", first.Published)
	}
	if strings.Contains(first.Content, "alert") {
		t.Fatalf("script survived conversion: %q", first.Content)
	}
	if !strings.Contains(first.Content, "Hello") || !strings.Contains(first.Content, "**world**") {
		t.Fatalf("markdown content = %q", first.Content)
	}
	if first.Summary == "" {
		t.Fatal("summary is empty")
	}
	if r.Items[1].Link != "https://elsewhere.example/second" {
		t.Fatalf("absolute link = %q", r.Items[1].Link)
	}
}

func TestFetchAtomUsesUpdated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, atomBody)
	}))
	defer srv.Close()

	r := newFetcher(2*time.Second).FetchAll(context.Background(), []config.Feed{{URL: srv.URL}}, nil)[0]
	if r.Err != nil {
		t.Fatalf("fetch: %v", r.Err)
	}
	if r.Title != "Atom Feed" {
		t.Fatalf("parsed title = %q", r.Title)
	}
	if len(r.Items) != 1 {
		t.Fatalf("items = %d", len(r.Items))
	}
	if r.Items[0].Link != "https://atom.example/a1" {
		t.Fatalf("atom link = %q", r.Items[0].Link)
	}
	if r.Items[0].Published.IsZero() {
		t.Fatal("atom updated date not used")
	}
	if !strings.Contains(r.Items[0].Content, "Atom") {
		t.Fatalf("atom content = %q", r.Items[0].Content)
	}
}

func TestFetchTimeoutIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		fmt.Fprint(w, rssBody)
	}))
	defer srv.Close()

	r := newFetcher(60*time.Millisecond).FetchAll(context.Background(), []config.Feed{{URL: srv.URL}}, nil)[0]
	if r.Err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(r.Err.Error(), "timed out") {
		t.Fatalf("error = %v", r.Err)
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := newFetcher(time.Second).FetchAll(context.Background(), []config.Feed{{URL: srv.URL}}, nil)[0]
	if r.Err == nil || !strings.Contains(r.Err.Error(), "HTTP 500") {
		t.Fatalf("error = %v", r.Err)
	}
}

func TestFetchAllKeepsInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>%s</title><item><title>%s</title><guid>%s</guid></item></channel></rss>`,
			r.URL.Path, r.URL.Path, r.URL.Path)
	}))
	defer srv.Close()

	feeds := []config.Feed{{URL: srv.URL + "/a"}, {URL: srv.URL + "/b"}, {URL: srv.URL + "/c"}}
	results := newFetcher(2*time.Second).FetchAll(context.Background(), feeds, nil)
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("feed %d: %v", i, r.Err)
		}
		if r.Feed.URL != feeds[i].URL {
			t.Fatalf("result %d URL = %q, want %q", i, r.Feed.URL, feeds[i].URL)
		}
		if len(r.Items) != 1 || r.Items[0].Title != feeds[i].URL[len(srv.URL):] {
			t.Fatalf("result %d items = %+v", i, r.Items)
		}
	}
}

func TestConvertItemFallbacks(t *testing.T) {
	item := convertItem(&gofeed.Item{}, nil)
	if item.Title != "(untitled)" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.GUID == "" {
		t.Fatal("guid fallback is empty")
	}
}

func TestToMarkdownFallback(t *testing.T) {
	md := toMarkdown("<p>plain <b>text</b></p>")
	if !strings.Contains(md, "plain") || !strings.Contains(md, "text") {
		t.Fatalf("markdown = %q", md)
	}
	if got := toMarkdown("   "); got != "" {
		t.Fatalf("empty markdown = %q", got)
	}
}

func TestFetchConditionalNotModified(t *testing.T) {
	const (
		etag    = `"v1"`
		lastMod = "Mon, 02 Jan 2023 15:04:05 GMT"
	)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == etag || r.Header.Get("If-Modified-Since") == lastMod {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		fmt.Fprint(w, rssBody)
	}))
	defer srv.Close()

	feeds := []config.Feed{{URL: srv.URL}}
	first := newFetcher(time.Second).FetchAll(context.Background(), feeds, nil)[0]
	if first.Err != nil {
		t.Fatalf("first fetch: %v", first.Err)
	}
	if first.NotModified {
		t.Fatal("first fetch reported a 304")
	}
	if first.Validators.ETag != etag || first.Validators.LastModified == "" {
		t.Fatalf("validators not captured: %+v", first.Validators)
	}
	if len(first.Items) != 2 {
		t.Fatalf("first items = %d", len(first.Items))
	}

	second := newFetcher(time.Second).FetchAll(context.Background(), feeds, map[string]Validators{
		srv.URL: first.Validators,
	})[0]
	if second.Err != nil {
		t.Fatalf("second fetch: %v", second.Err)
	}
	if !second.NotModified {
		t.Fatal("second fetch did not report a 304")
	}
	if len(second.Items) != 0 {
		t.Fatalf("304 returned %d items", len(second.Items))
	}

	lastModified := newFetcher(time.Second).FetchAll(context.Background(), feeds, map[string]Validators{
		srv.URL: {LastModified: first.Validators.LastModified},
	})[0]
	if !lastModified.NotModified {
		t.Fatalf("Last-Modified was not revalidated: %+v", lastModified.Validators)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestFetchReplacesStaleValidators(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"old"` {
			w.Header().Set("ETag", `"new"`)
			fmt.Fprint(w, rssBody)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	r := newFetcher(time.Second).FetchAll(context.Background(),
		[]config.Feed{{URL: srv.URL}},
		map[string]Validators{srv.URL: {ETag: `"old"`}})[0]
	if r.Err != nil || r.NotModified {
		t.Fatalf("expected a fresh body, got err=%v notModified=%v", r.Err, r.NotModified)
	}
	if r.Validators.ETag != `"new"` {
		t.Fatalf("etag = %q", r.Validators.ETag)
	}
}
