package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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

func TestStarredRoundTripAndFilter(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now()
	if _, err := st.SyncFeed(ctx, IngestFeed{
		URL: "https://example.com/rss", Title: "E", Category: "tech",
		Articles: []IngestArticle{article("a", now), article("b", now.Add(time.Hour))},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	arts, _ := st.Articles(ctx, ListOptions{})
	if len(arts) != 2 {
		t.Fatalf("articles = %d", len(arts))
	}
	for _, a := range arts {
		if a.Starred {
			t.Fatalf("fresh article is starred: %+v", a)
		}
	}

	oldest := arts[1]
	for _, a := range arts {
		if a.GUID == "a" {
			oldest = a
		}
	}
	if err := st.SetStarred(ctx, []int64{oldest.ID}, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	starred, err := st.Articles(ctx, ListOptions{StarredOnly: true})
	if err != nil || len(starred) != 1 {
		t.Fatalf("starred filter = %d, err %v", len(starred), err)
	}
	if starred[0].ID != oldest.ID || !starred[0].Starred {
		t.Fatalf("starred filter returned %+v", starred[0])
	}

	if err := st.MarkRead(ctx, []int64{oldest.ID}, true); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	combo, err := st.Articles(ctx, ListOptions{StarredOnly: true, UnreadOnly: true})
	if err != nil || len(combo) != 0 {
		t.Fatalf("starred+unread = %d, err %v", len(combo), err)
	}

	if err := st.SetStarred(ctx, []int64{oldest.ID}, false); err != nil {
		t.Fatalf("unstar: %v", err)
	}
	starred, err = st.Articles(ctx, ListOptions{StarredOnly: true})
	if err != nil || len(starred) != 0 {
		t.Fatalf("unstar left %d rows, err %v", len(starred), err)
	}
	arts, _ = st.Articles(ctx, ListOptions{})
	for _, a := range arts {
		if a.ID != oldest.ID {
			continue
		}
		if !a.Read || a.Starred {
			t.Fatalf("unstar disturbed read state or kept the star: %+v", a)
		}
	}
}

func TestPruneKeepsStarred(t *testing.T) {
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
	arts, _ := st.Articles(ctx, ListOptions{})
	var oldest int64
	for _, a := range arts {
		if a.GUID == "g0" {
			oldest = a.ID
		}
	}
	if err := st.SetStarred(ctx, []int64{oldest}, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	if err := st.Prune(ctx, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("articles: %v", err)
	}
	if len(arts) != 3 {
		t.Fatalf("after prune = %d, want the starred row plus the cap", len(arts))
	}
	guids := []string{arts[0].GUID, arts[1].GUID, arts[2].GUID}
	want := []string{"g4", "g3", "g0"}
	for i := range want {
		if guids[i] != want[i] {
			t.Fatalf("kept %v, want %v", guids, want)
		}
	}
	if !arts[2].Starred {
		t.Fatalf("the surviving old row lost its star: %+v", arts[2])
	}
}

func TestPruneKeepsAllStarred(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now()
	var items []IngestArticle
	for i := 0; i < 3; i++ {
		items = append(items, article(fmt.Sprintf("s%d", i), now.Add(time.Duration(i)*time.Minute)))
	}
	if _, err := st.SyncFeed(ctx, IngestFeed{URL: "https://example.com/rss", Title: "E", Category: "tech", Articles: items}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	arts, _ := st.Articles(ctx, ListOptions{})
	ids := make([]int64, 0, len(arts))
	for _, a := range arts {
		ids = append(ids, a.ID)
	}
	if err := st.SetStarred(ctx, ids, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	if err := st.Prune(ctx, 1); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("articles: %v", err)
	}
	if len(arts) != 3 {
		t.Fatalf("prune deleted a starred row: %d left", len(arts))
	}
	for _, a := range arts {
		if !a.Starred {
			t.Fatalf("a surviving row lost its star: %+v", a)
		}
	}
}

func TestFreshSchemaCreatesIndexes(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	for _, name := range []string{"idx_articles_published", "idx_articles_unread", "idx_articles_starred"} {
		var n int
		if err := st.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("read index %s: %v", name, err)
		}
		if n != 1 {
			t.Fatalf("fresh schema is missing %s", name)
		}
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

func TestFeedsReportsHealthAndValidators(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now()
	if _, err := st.SyncFeed(ctx, IngestFeed{
		URL: "https://example.com/rss", Title: "Example", Category: "tech",
		ETag: `"abc"`, LastModified: "Mon, 02 Jan 2023 15:04:05 GMT",
		Articles: []IngestArticle{article("a", now), article("b", now.Add(time.Hour))},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := st.RecordFeedError(ctx, "https://broken.example/rss", "Broken", "news", "HTTP 500"); err != nil {
		t.Fatalf("record: %v", err)
	}
	feeds, err := st.Feeds(ctx)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %d, want 2", len(feeds))
	}
	if feeds[0].Title != "Broken" || feeds[0].LastError != "HTTP 500" || feeds[0].Articles != 0 {
		t.Fatalf("broken feed = %+v", feeds[0])
	}
	example := feeds[1]
	if example.ETag != `"abc"` || example.LastModified == "" {
		t.Fatalf("validators = %+v", example)
	}
	if example.Unread != 2 || example.Articles != 2 {
		t.Fatalf("counters = %d unread / %d articles", example.Unread, example.Articles)
	}
	if example.LastFetched.IsZero() {
		t.Fatal("LastFetched is zero after a successful sync")
	}

	arts, _ := st.Articles(ctx, ListOptions{})
	if err := st.MarkRead(ctx, []int64{arts[0].ID}, true); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	feeds, _ = st.Feeds(ctx)
	for _, f := range feeds {
		if f.URL == example.URL && f.Unread != 1 {
			t.Fatalf("unread after mark read = %d", f.Unread)
		}
	}
}

// TestValidatorsSurviveReopen proves the conditional-fetch state comes from
// disk, not from process memory, so a restarted reader still sends If-None-Match.
func TestValidatorsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.SyncFeed(ctx, IngestFeed{
		URL: "https://example.com/rss", Title: "Example", Category: "tech",
		ETag: `"abc"`, LastModified: "Mon, 02 Jan 2023 15:04:05 GMT",
	}); err != nil {
		st.Close()
		t.Fatalf("sync: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	feeds, err := reopened.Feeds(ctx)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("feeds = %d, want 1", len(feeds))
	}
	if feeds[0].ETag != `"abc"` || feeds[0].LastModified != "Mon, 02 Jan 2023 15:04:05 GMT" {
		t.Fatalf("validators lost across restart: %+v", feeds[0])
	}
}

func TestRecordFeedErrorFollowsConfigAndClears(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	const url = "https://example.com/rss"
	if err := st.RecordFeedError(ctx, url, "Old name", "tech", "timed out"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := st.RecordFeedError(ctx, url, "New name", "news", "still down"); err != nil {
		t.Fatalf("record again: %v", err)
	}
	feeds, _ := st.Feeds(ctx)
	if len(feeds) != 1 {
		t.Fatalf("feeds = %d", len(feeds))
	}
	if feeds[0].Title != "New name" || feeds[0].Category != "news" {
		t.Fatalf("a failing feed kept stale config: %+v", feeds[0])
	}
	if feeds[0].LastError != "still down" {
		t.Fatalf("last error = %q", feeds[0].LastError)
	}

	if err := st.TouchFeed(ctx, url, "New name", "news"); err != nil {
		t.Fatalf("TouchFeed: %v", err)
	}
	feeds, _ = st.Feeds(ctx)
	if feeds[0].LastError != "" {
		t.Fatalf("304 did not clear the error: %q", feeds[0].LastError)
	}
	if feeds[0].Title != "New name" || feeds[0].Category != "news" {
		t.Fatalf("304 did not adopt the config change: %+v", feeds[0])
	}

	// A feed with no configured title keeps the title parsed from its body.
	if _, err := st.SyncFeed(ctx, IngestFeed{URL: url, Title: "Parsed Title", Category: "news"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := st.TouchFeed(ctx, url, "", "news"); err != nil {
		t.Fatalf("TouchFeed: %v", err)
	}
	feeds, _ = st.Feeds(ctx)
	if feeds[0].Title != "Parsed Title" {
		t.Fatalf("304 downgraded the parsed title: %q", feeds[0].Title)
	}

	if err := st.RecordFeedError(ctx, url, "New name", "news", "down again"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := st.SyncFeed(ctx, IngestFeed{URL: url, Title: "New name", Category: "news"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	feeds, _ = st.Feeds(ctx)
	if feeds[0].LastError != "" {
		t.Fatalf("successful sync kept the error: %q", feeds[0].LastError)
	}
}

const schemaV1 = `
CREATE TABLE feeds (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	url          TEXT NOT NULL UNIQUE,
	title        TEXT NOT NULL DEFAULT '',
	category     TEXT NOT NULL DEFAULT 'general',
	last_fetched INTEGER,
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE articles (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	feed_id    INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
	guid       TEXT NOT NULL,
	title      TEXT NOT NULL,
	link       TEXT NOT NULL DEFAULT '',
	author     TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	content    TEXT NOT NULL DEFAULT '',
	published  INTEGER NOT NULL,
	first_seen INTEGER NOT NULL,
	is_read    INTEGER NOT NULL DEFAULT 0,
	UNIQUE(feed_id, guid)
);
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`

func TestMigrateFromV1KeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	for _, stmt := range []string{
		schemaV1,
		`INSERT INTO feeds (url, title, category, last_fetched) VALUES ('https://old.example/rss', 'Old', 'tech', 1700000000)`,
		`INSERT INTO articles (feed_id, guid, title, published, first_seen, is_read)
			VALUES (1, 'g1', 'Cached', 1700000000, 1700000000, 1)`,
		"PRAGMA user_version=1",
	} {
		if _, err := raw.Exec(stmt); err != nil {
			raw.Close()
			t.Fatalf("seed v1: %v", err)
		}
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open after v1: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	var version int
	if err := st.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
	feeds, err := st.Feeds(ctx)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if len(feeds) != 1 || feeds[0].Title != "Old" {
		t.Fatalf("migration lost the feed: %+v", feeds)
	}
	if feeds[0].Articles != 1 || feeds[0].Unread != 0 {
		t.Fatalf("migration lost the read state: %+v", feeds[0])
	}
	if feeds[0].ETag != "" || feeds[0].LastModified != "" {
		t.Fatalf("new columns not empty: %+v", feeds[0])
	}

	if _, err := st.SyncFeed(ctx, IngestFeed{URL: feeds[0].URL, Title: "Old", Category: "tech", ETag: `"x"`}); err != nil {
		t.Fatalf("sync after migration: %v", err)
	}
	feeds, _ = st.Feeds(ctx)
	if feeds[0].ETag != `"x"` {
		t.Fatalf("validators unwritable after migration: %+v", feeds[0])
	}

	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil || len(arts) != 1 {
		t.Fatalf("articles after migration: %v %d", err, len(arts))
	}
	if arts[0].Starred {
		t.Fatal("migrated article is starred")
	}
	if err := st.SetStarred(ctx, []int64{arts[0].ID}, true); err != nil {
		t.Fatalf("SetStarred after migration: %v", err)
	}
	starred, err := st.Articles(ctx, ListOptions{StarredOnly: true})
	if err != nil || len(starred) != 1 {
		t.Fatalf("starred after migration: %v %d", err, len(starred))
	}
}

const schemaV2 = `
CREATE TABLE feeds (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	url           TEXT NOT NULL UNIQUE,
	title         TEXT NOT NULL DEFAULT '',
	category      TEXT NOT NULL DEFAULT 'general',
	last_fetched  INTEGER,
	last_error    TEXT NOT NULL DEFAULT '',
	etag          TEXT NOT NULL DEFAULT '',
	last_modified TEXT NOT NULL DEFAULT ''
);
CREATE TABLE articles (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	feed_id    INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
	guid       TEXT NOT NULL,
	title      TEXT NOT NULL,
	link       TEXT NOT NULL DEFAULT '',
	author     TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	content    TEXT NOT NULL DEFAULT '',
	published  INTEGER NOT NULL,
	first_seen INTEGER NOT NULL,
	is_read    INTEGER NOT NULL DEFAULT 0,
	UNIQUE(feed_id, guid)
);
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`

func TestMigrateFromV2AddsStarred(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	for _, stmt := range []string{
		schemaV2,
		`INSERT INTO feeds (url, title, category, etag) VALUES ('https://v2.example/rss', 'V2', 'tech', '"e"')`,
		`INSERT INTO articles (feed_id, guid, title, published, first_seen, is_read)
			VALUES (1, 'g1', 'Kept', 1700000000, 1700000000, 1)`,
		"PRAGMA user_version=2",
	} {
		if _, err := raw.Exec(stmt); err != nil {
			raw.Close()
			t.Fatalf("seed v2: %v", err)
		}
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open after v2: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	var version int
	if err := st.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
	var indexes int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_articles_starred'`).Scan(&indexes); err != nil {
		t.Fatalf("read index: %v", err)
	}
	if indexes != 1 {
		t.Fatal("v2 to v3 did not create the starred index")
	}
	arts, err := st.Articles(ctx, ListOptions{})
	if err != nil || len(arts) != 1 {
		t.Fatalf("articles after v2 upgrade: %v %d", err, len(arts))
	}
	if !arts[0].Read || arts[0].Starred {
		t.Fatalf("v2 read state or starred default wrong: %+v", arts[0])
	}
	feeds, _ := st.Feeds(ctx)
	if feeds[0].ETag != `"e"` {
		t.Fatalf("v2 validators lost: %+v", feeds[0])
	}
	if err := st.SetStarred(ctx, []int64{arts[0].ID}, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec("PRAGMA user_version=99"); err != nil {
		raw.Close()
		t.Fatalf("seed version: %v", err)
	}
	raw.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open returned %v", err)
	}
}
