package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func article(guid string, published time.Time) IngestArticle {
	return IngestArticle{
		GUID:      guid,
		Title:     "Title " + guid,
		Link:      "https://example.com/" + guid,
		Content:   "Body **" + guid + "**",
		Summary:   "Body " + guid,
		Published: published,
	}
}

func TestSyncFeedIdempotentAndReadState(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	n, err := st.SyncFeed(ctx, IngestFeed{
		URL: "https://example.com/rss", Title: "Example", Category: "tech",
		Articles: []IngestArticle{article("a", now), article("b", now.Add(time.Hour))},
	})
	if err != nil {
		t.Fatalf("SyncFeed: %v", err)
	}
	if n != 2 {
		t.Fatalf("first sync added %d, want 2", n)
	}

	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("Articles: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("articles = %d", len(arts))
	}
	if arts[0].FeedTitle != "Example" || arts[0].FeedCategory != "tech" {
		t.Fatalf("feed metadata = %q/%q", arts[0].FeedTitle, arts[0].FeedCategory)
	}
	if arts[0].GUID != "b" {
		t.Fatalf("newest first: got %q", arts[0].GUID)
	}

	if err := st.MarkRead(ctx, []int64{arts[0].ID}, true); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if n, err := st.UnreadCount(ctx); err != nil || n != 1 {
		t.Fatalf("unread = %d, err %v", n, err)
	}

	n, err = st.SyncFeed(ctx, IngestFeed{
		URL: "https://example.com/rss", Title: "Example", Category: "tech",
		Articles: []IngestArticle{article("a", now), article("b", now.Add(time.Hour)), article("c", now.Add(2*time.Hour))},
	})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("second sync added %d, want 1", n)
	}
	arts, _ = st.Articles(ctx, ListOptions{})
	for _, a := range arts {
		if a.GUID == "b" && !a.Read {
			t.Fatal("read state lost on re-sync")
		}
	}

	unread, err := st.Articles(ctx, ListOptions{UnreadOnly: true})
	if err != nil {
		t.Fatalf("unread filter: %v", err)
	}
	if len(unread) != 2 {
		t.Fatalf("unread filter = %d, want 2", len(unread))
	}
	if err := st.MarkRead(ctx, []int64{unread[0].ID, unread[1].ID}, true); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Fatalf("unread after mark all = %d", n)
	}
}

func TestCategoryFilterAndLimit(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now()
	for i, cat := range []string{"tech", "news"} {
		_, err := st.SyncFeed(ctx, IngestFeed{
			URL: fmt.Sprintf("https://example.com/%d", i), Title: cat, Category: cat,
			Articles: []IngestArticle{article(cat, now.Add(time.Duration(i)*time.Minute))},
		})
		if err != nil {
			t.Fatalf("sync %s: %v", cat, err)
		}
	}
	tech, err := st.Articles(ctx, ListOptions{Category: "tech"})
	if err != nil {
		t.Fatalf("category filter: %v", err)
	}
	if len(tech) != 1 || tech[0].FeedCategory != "tech" {
		t.Fatalf("category filter = %+v", tech)
	}
	limited, err := st.Articles(ctx, ListOptions{Limit: 1})
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit = %d, err %v", len(limited), err)
	}
}

func TestPruneKeepsNewestPerFeed(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now()
	var items []IngestArticle
	for i := 0; i < 5; i++ {
		items = append(items, article(fmt.Sprintf("g%d", i), now.Add(time.Duration(i)*time.Minute)))
	}
	if _, err := st.SyncFeed(ctx, IngestFeed{URL: "https://example.com/rss", Title: "E", Category: "tech", Articles: items}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := st.Prune(ctx, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("articles: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("after prune = %d, want 2", len(arts))
	}
	if arts[0].GUID != "g4" || arts[1].GUID != "g3" {
		t.Fatalf("kept %q/%q", arts[0].GUID, arts[1].GUID)
	}
}

func TestMetaAndFeedErrors(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, ok, err := st.Meta(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing meta ok=%v err=%v", ok, err)
	}
	if err := st.SetMeta(ctx, "theme", "nord"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := st.SetMeta(ctx, "theme", "dracula"); err != nil {
		t.Fatalf("SetMeta overwrite: %v", err)
	}
	if v, ok, err := st.Meta(ctx, "theme"); err != nil || !ok || v != "dracula" {
		t.Fatalf("meta = %q/%v/%v", v, ok, err)
	}

	if err := st.RecordFeedError(ctx, "https://bad.example/rss", "Bad", "tech", "timed out"); err != nil {
		t.Fatalf("RecordFeedError: %v", err)
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Fatalf("feed error created %d articles", n)
	}
	if err := st.PruneFeeds(ctx, []string{"https://other.example/rss"}); err != nil {
		t.Fatalf("PruneFeeds: %v", err)
	}
}

func TestPruneFeedsCascadesArticles(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.SyncFeed(ctx, IngestFeed{URL: "https://a.example/rss", Title: "A", Category: "tech", Articles: []IngestArticle{article("x", time.Now())}}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := st.PruneFeeds(ctx, []string{"https://b.example/rss"}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	arts, _ := st.Articles(ctx, ListOptions{})
	if len(arts) != 0 {
		t.Fatalf("orphan articles = %d", len(arts))
	}
}
