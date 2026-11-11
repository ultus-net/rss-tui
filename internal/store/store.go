// Package store persists feeds, cached articles and read state in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS feeds (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	url          TEXT NOT NULL UNIQUE,
	title        TEXT NOT NULL DEFAULT '',
	category     TEXT NOT NULL DEFAULT 'general',
	last_fetched INTEGER,
	last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS articles (
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

CREATE INDEX IF NOT EXISTS idx_articles_published ON articles(published DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_articles_unread ON articles(is_read, published DESC);

CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// Feed is a subscription row.
type Feed struct {
	ID          int64
	URL         string
	Title       string
	Category    string
	LastFetched time.Time
	LastError   string
}

// Article is a cached item joined with its feed metadata.
type Article struct {
	ID           int64
	FeedID       int64
	GUID         string
	Title        string
	Link         string
	Author       string
	Summary      string
	Content      string
	Published    time.Time
	FirstSeen    time.Time
	Read         bool
	FeedTitle    string
	FeedCategory string
}

// IngestFeed is one fetch result ready to persist.
type IngestFeed struct {
	URL      string
	Title    string
	Category string
	Articles []IngestArticle
}

// IngestArticle is one parsed article.
type IngestArticle struct {
	GUID      string
	Title     string
	Link      string
	Author    string
	Summary   string
	Content   string
	Published time.Time
}

// ListOptions narrows an article query.
type ListOptions struct {
	UnreadOnly bool
	Category   string
	Limit      int
}

// Store owns the SQLite connection. It is safe for concurrent use; writes are
// serialised through a single connection.
type Store struct {
	db *sql.DB
}

// Open creates the database and applies the schema if needed.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty database path")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: create data dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("store: database schema v%d is newer than this build (v%d)", version, schemaVersion)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: apply schema: %w", err)
	}
	if version < schemaVersion {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
			return fmt.Errorf("store: set schema version: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// SyncFeed upserts a feed and inserts unseen articles, returning how many were
// newly added. Existing articles keep their read state.
func (s *Store) SyncFeed(ctx context.Context, f IngestFeed) (int, error) {
	if strings.TrimSpace(f.URL) == "" {
		return 0, errors.New("store: feed URL is required")
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM feeds WHERE url = ?", f.URL).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, insErr := tx.ExecContext(ctx,
			"INSERT INTO feeds (url, title, category, last_fetched, last_error) VALUES (?, ?, ?, ?, '')",
			f.URL, f.Title, f.Category, now)
		if insErr != nil {
			return 0, fmt.Errorf("store: insert feed: %w", insErr)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("store: feed id: %w", err)
		}
	case err != nil:
		return 0, fmt.Errorf("store: lookup feed: %w", err)
	default:
		if _, err := tx.ExecContext(ctx,
			"UPDATE feeds SET title = ?, category = ?, last_fetched = ?, last_error = '' WHERE id = ?",
			f.Title, f.Category, now, id); err != nil {
			return 0, fmt.Errorf("store: update feed: %w", err)
		}
	}

	newCount := 0
	for _, a := range f.Articles {
		published := a.Published
		if published.IsZero() {
			published = time.Now()
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO articles (feed_id, guid, title, link, author, summary, content, published, first_seen, is_read)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
			ON CONFLICT(feed_id, guid) DO NOTHING`,
			id, a.GUID, a.Title, a.Link, a.Author, a.Summary, a.Content, published.Unix(), now)
		if err != nil {
			return 0, fmt.Errorf("store: insert article %q: %w", a.Title, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: article rows: %w", err)
		}
		newCount += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return newCount, nil
}

// RecordFeedError stores the last failure for a feed so it is visible after a
// restart. It does not create an unread article.
func (s *Store) RecordFeedError(ctx context.Context, url, title, category, message string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feeds (url, title, category, last_fetched, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			last_fetched = excluded.last_fetched,
			last_error = excluded.last_error`,
		url, title, category, now, message)
	if err != nil {
		return fmt.Errorf("store: record feed error: %w", err)
	}
	return nil
}

// Articles returns cached articles, newest first.
func (s *Store) Articles(ctx context.Context, opts ListOptions) ([]Article, error) {
	var q strings.Builder
	q.WriteString(`SELECT a.id, a.feed_id, a.guid, a.title, a.link, a.author, a.summary, a.content,
		a.published, a.first_seen, a.is_read, f.title, f.category
		FROM articles a JOIN feeds f ON f.id = a.feed_id WHERE 1 = 1`)
	args := make([]any, 0, 3)
	if opts.UnreadOnly {
		q.WriteString(" AND a.is_read = 0")
	}
	if opts.Category != "" {
		q.WriteString(" AND f.category = ?")
		args = append(args, opts.Category)
	}
	q.WriteString(" ORDER BY a.published DESC, a.id DESC")
	if opts.Limit > 0 {
		q.WriteString(" LIMIT ?")
		args = append(args, opts.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list articles: %w", err)
	}
	defer rows.Close()

	var out []Article
	for rows.Next() {
		var a Article
		var published, firstSeen int64
		if err := rows.Scan(&a.ID, &a.FeedID, &a.GUID, &a.Title, &a.Link, &a.Author,
			&a.Summary, &a.Content, &published, &firstSeen, &a.Read, &a.FeedTitle, &a.FeedCategory); err != nil {
			return nil, fmt.Errorf("store: scan article: %w", err)
		}
		a.Published = time.Unix(published, 0)
		a.FirstSeen = time.Unix(firstSeen, 0)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate articles: %w", err)
	}
	return out, nil
}

// MarkRead sets read state for the given article IDs.
func (s *Store) MarkRead(ctx context.Context, ids []int64, read bool) error {
	if len(ids) == 0 {
		return nil
	}
	value := 0
	if read {
		value = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback()
	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		args = append(args, value)
		for _, id := range batch {
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE articles SET is_read = ? WHERE id IN ("+placeholders+")", args...); err != nil {
			return fmt.Errorf("store: mark read: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// UnreadCount is the number of unread articles across every feed.
func (s *Store) UnreadCount(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM articles WHERE is_read = 0").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: unread count: %w", err)
	}
	return n, nil
}

// Prune keeps only the newest perFeed articles per feed.
func (s *Store) Prune(ctx context.Context, perFeed int) error {
	if perFeed <= 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM articles WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY feed_id ORDER BY published DESC, id DESC
				) AS rn FROM articles
			) AS ranked WHERE rn > ?
		)`, perFeed); err != nil {
		return fmt.Errorf("store: prune: %w", err)
	}
	return nil
}

// PruneFeeds removes feeds (and their articles) no longer present in config.
// An empty URL list clears the cache.
func (s *Store) PruneFeeds(ctx context.Context, urls []string) error {
	if len(urls) == 0 {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM feeds"); err != nil {
			return fmt.Errorf("store: clear feeds: %w", err)
		}
		return nil
	}
	args := make([]any, len(urls))
	placeholders := make([]string, len(urls))
	for i, u := range urls {
		args[i] = u
		placeholders[i] = "?"
	}
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM feeds WHERE url NOT IN ("+strings.Join(placeholders, ",")+")", args...); err != nil {
		return fmt.Errorf("store: prune feeds: %w", err)
	}
	return nil
}

// Meta reads a metadata value.
func (s *Store) Meta(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: read meta %q: %w", key, err)
	}
	return value, true, nil
}

// SetMeta writes a metadata value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value); err != nil {
		return fmt.Errorf("store: write meta %q: %w", key, err)
	}
	return nil
}
