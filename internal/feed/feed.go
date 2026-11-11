// Package feed fetches and parses RSS/Atom feeds concurrently.
package feed

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/mmcdole/gofeed"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/textutil"
)

const (
	defaultMaxBody   = 8 << 20 // 8 MiB
	maxContentRunes  = 20000
	summaryMaxCells  = 240
	defaultUserAgent = "rss-tui/1.0 (+https://github.com/ultus-net/rss-tui)"
)

var noisyTagsRE = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)

// Item is one parsed article, already cleaned for terminal display.
type Item struct {
	GUID      string
	Title     string
	Link      string
	Author    string
	Summary   string
	Content   string
	Published time.Time
}

// Validators are the HTTP cache validators a feed sent last time. Sending them
// back lets the server answer 304 and skip the body, parse and conversion.
type Validators struct {
	ETag         string
	LastModified string
}

// Result is the outcome of fetching one feed.
type Result struct {
	Feed        config.Feed
	Title       string
	Items       []Item
	Err         error
	Elapsed     time.Duration
	Validators  Validators
	NotModified bool
}

// Options configures a Fetcher.
type Options struct {
	Timeout   time.Duration
	Workers   int
	UserAgent string
	MaxBody   int64
}

// Fetcher runs a bounded worker pool over the configured feeds.
type Fetcher struct {
	client    *http.Client
	timeout   time.Duration
	workers   int
	userAgent string
	maxBody   int64
}

func New(opts Options) *Fetcher {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = config.DefaultPerFeedTimeout
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = config.DefaultConcurrentFetches
	}
	agent := strings.TrimSpace(opts.UserAgent)
	if agent == "" {
		agent = defaultUserAgent
	}
	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	return &Fetcher{
		client:    &http.Client{Timeout: timeout},
		timeout:   timeout,
		workers:   workers,
		userAgent: agent,
		maxBody:   maxBody,
	}
}

// FetchAll fetches feeds concurrently and returns one result per feed in input
// order. A slow or failing feed never blocks the others. Pass the validators
// from the previous poll per feed URL; a 304 short-circuits the body read.
func (f *Fetcher) FetchAll(ctx context.Context, feeds []config.Feed, validators map[string]Validators) []Result {
	results := make([]Result, len(feeds))
	sem := make(chan struct{}, f.workers)
	var wg sync.WaitGroup
	for i := range feeds {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = f.fetchOne(ctx, feeds[i], validators[feeds[i].URL])
		}(i)
	}
	wg.Wait()
	return results
}

func (f *Fetcher) fetchOne(parent context.Context, feed config.Feed, cached Validators) Result {
	started := time.Now()
	res := Result{Feed: feed, Title: feed.DisplayName(), Validators: cached}
	ctx, cancel := context.WithTimeout(parent, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		res.Err = fmt.Errorf("build request: %w", err)
		res.Elapsed = time.Since(started)
		return res
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.8")
	if cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}
	if cached.LastModified != "" {
		req.Header.Set("If-Modified-Since", cached.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		res.Err = fetchError(ctx, err, f.timeout)
		res.Elapsed = time.Since(started)
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		res.NotModified = true
		res.Elapsed = time.Since(started)
		return res
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		res.Err = fmt.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		res.Elapsed = time.Since(started)
		return res
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBody+1))
	if err != nil {
		res.Err = fetchError(ctx, fmt.Errorf("read body: %w", err), f.timeout)
		res.Elapsed = time.Since(started)
		return res
	}
	if int64(len(body)) > f.maxBody {
		res.Err = fmt.Errorf("feed larger than %d MiB", f.maxBody>>20)
		res.Elapsed = time.Since(started)
		return res
	}

	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		res.Err = fmt.Errorf("parse feed: %w", err)
		res.Elapsed = time.Since(started)
		return res
	}
	if strings.TrimSpace(parsed.Title) != "" && strings.TrimSpace(feed.Title) == "" {
		res.Title = textutil.OneLine(parsed.Title)
	}
	base := feedBaseURL(parsed.Link, feed.URL)

	seen := make(map[string]bool, len(parsed.Items))
	for _, it := range parsed.Items {
		item := convertItem(it, base)
		if item.GUID == "" || seen[item.GUID] {
			continue
		}
		seen[item.GUID] = true
		res.Items = append(res.Items, item)
	}
	res.Validators = Validators{
		ETag:         strings.TrimSpace(resp.Header.Get("ETag")),
		LastModified: strings.TrimSpace(resp.Header.Get("Last-Modified")),
	}
	res.Elapsed = time.Since(started)
	return res
}

func fetchError(ctx context.Context, err error, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s", timeout)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return err
}

func feedBaseURL(feedLink, feedURL string) *url.URL {
	if strings.TrimSpace(feedLink) != "" {
		if u, err := url.Parse(strings.TrimSpace(feedLink)); err == nil && u.IsAbs() {
			return u
		}
	}
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil
	}
	return u
}

func convertItem(it *gofeed.Item, base *url.URL) Item {
	title := textutil.Clean(strings.TrimSpace(it.Title))
	if title == "" {
		title = "(untitled)"
	}
	link := resolveLink(it.Link, base)
	guid := strings.TrimSpace(it.GUID)
	if guid == "" {
		guid = link
	}
	published := time.Time{}
	if it.PublishedParsed != nil {
		published = it.PublishedParsed.UTC()
	} else if it.UpdatedParsed != nil {
		published = it.UpdatedParsed.UTC()
	}
	if guid == "" {
		guid = fallbackGUID(title, link, published)
	}
	author := authorName(it)
	htmlBody := it.Content
	if strings.TrimSpace(htmlBody) == "" {
		htmlBody = it.Description
	}
	content := toMarkdown(htmlBody)
	return Item{
		GUID:      guid,
		Title:     title,
		Link:      link,
		Author:    textutil.Clean(author),
		Summary:   textutil.Summary(content, summaryMaxCells),
		Content:   content,
		Published: published,
	}
}

func resolveLink(raw string, base *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if !u.IsAbs() && base != nil {
		return base.ResolveReference(u).String()
	}
	return u.String()
}

func authorName(it *gofeed.Item) string {
	for _, a := range it.Authors {
		if a != nil && strings.TrimSpace(a.Name) != "" {
			return a.Name
		}
	}
	if it.Author != nil {
		return it.Author.Name
	}
	return ""
}

func fallbackGUID(title, link string, published time.Time) string {
	sum := sha1.Sum([]byte(title + "\x00" + link + "\x00" + published.Format(time.RFC3339)))
	return hex.EncodeToString(sum[:])
}

// toMarkdown converts feed HTML into readable markdown, capped for the cache.
func toMarkdown(rawHTML string) string {
	rawHTML = strings.TrimSpace(rawHTML)
	if rawHTML == "" {
		return ""
	}
	rawHTML = noisyTagsRE.ReplaceAllString(rawHTML, " ")
	md, err := htmltomarkdown.ConvertString(rawHTML)
	if err != nil {
		md = stripTags(rawHTML)
	}
	md = textutil.Clean(md)
	runes := []rune(md)
	if len(runes) > maxContentRunes {
		md = strings.TrimSpace(string(runes[:maxContentRunes])) + "\n\n[content truncated]"
	}
	return md
}

var tagStripRE = regexp.MustCompile(`(?s)<[^>]*>`)

func stripTags(s string) string {
	return textutil.Clean(tagStripRE.ReplaceAllString(s, " "))
}
