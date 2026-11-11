package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadCreatesSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, created, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for a missing file")
	}
	if len(cfg.Feeds) != 4 {
		t.Fatalf("sample feeds = %d, want 4", len(cfg.Feeds))
	}
	if cfg.RefreshInterval.Std() != 10*time.Minute {
		t.Fatalf("refresh = %s, want 10m", cfg.RefreshInterval)
	}
	if cfg.Notifications {
		t.Fatal("sample must ship notifications off")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sample not written: %v", err)
	}
	again, created, err := Load(path)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if created {
		t.Fatal("second Load should not report creation")
	}
	if len(again.Feeds) != 4 {
		t.Fatalf("second load feeds = %d", len(again.Feeds))
	}
}

func TestDurationParsing(t *testing.T) {
	cfg, err := parse([]byte(strings.Join([]string{
		"refresh_interval: 90s",
		"per_feed_timeout: 5s",
		"feeds:",
		"  - url: https://example.com/rss",
	}, "\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.RefreshInterval.Std() != 90*time.Second {
		t.Fatalf("refresh = %s", cfg.RefreshInterval)
	}
	if cfg.PerFeedTimeout.Std() != 5*time.Second {
		t.Fatalf("timeout = %s", cfg.PerFeedTimeout)
	}
	if cfg.ConcurrentFetches != DefaultConcurrentFetches {
		t.Fatalf("workers default = %d", cfg.ConcurrentFetches)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "refresh_interval: 1m30s") {
		t.Fatalf("duration did not marshal as a string:\n%s", out)
	}
}

func TestDurationRejectsGarbage(t *testing.T) {
	_, err := parse([]byte("refresh_interval: soon\nfeeds:\n  - url: https://example.com/rss\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid duration")
	}
}

func TestValidate(t *testing.T) {
	base := func() Config {
		cfg := Config{
			RefreshInterval:    Duration(10 * time.Minute),
			PerFeedTimeout:     Duration(20 * time.Second),
			ConcurrentFetches:  6,
			MaxArticlesPerFeed: 100,
			PassiveItems:       20,
			Feeds:              []Feed{{URL: "https://example.com/rss", Category: "tech"}},
		}
		return cfg
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	noFeeds := base()
	noFeeds.Feeds = nil
	if err := noFeeds.Validate(); err == nil {
		t.Fatal("expected error for no feeds")
	}

	badURL := base()
	badURL.Feeds = []Feed{{URL: "ftp://example.com/rss"}}
	if err := badURL.Validate(); err == nil {
		t.Fatal("expected error for non-http URL")
	}

	fast := base()
	fast.RefreshInterval = Duration(time.Second)
	if err := fast.Validate(); err == nil {
		t.Fatal("expected error for sub-30s refresh")
	}
}

func TestDefaultsAndCategories(t *testing.T) {
	cfg := Config{Feeds: []Feed{
		{URL: "https://a.example/rss", Category: "Tech"},
		{URL: "https://b.example/rss", Category: "tech"},
		{URL: "https://c.example/rss"},
		{URL: "https://a.example/rss", Category: "dup"},
		{URL: " ", Category: "empty"},
	}}
	cfg.applyDefaults()
	if cfg.Theme != DefaultTheme {
		t.Fatalf("theme = %q", cfg.Theme)
	}
	if len(cfg.Feeds) != 3 {
		t.Fatalf("deduped feeds = %d, want 3", len(cfg.Feeds))
	}
	if cfg.Feeds[2].Category != "general" {
		t.Fatalf("empty category = %q, want general", cfg.Feeds[2].Category)
	}
	cats := cfg.Categories()
	if len(cats) != 2 || cats[0] != "tech" || cats[1] != "general" {
		t.Fatalf("categories = %v", cats)
	}
}

func TestDisplayName(t *testing.T) {
	f := Feed{URL: "https://news.example.com/rss"}
	if got := f.DisplayName(); got != "news.example.com" {
		t.Fatalf("DisplayName = %q", got)
	}
	f.Title = "  Example  "
	if got := f.DisplayName(); got != "Example" {
		t.Fatalf("DisplayName = %q", got)
	}
}

func TestSampleParses(t *testing.T) {
	cfg, err := parse([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(cfg.Feeds) != 4 {
		t.Fatalf("sample feeds = %d", len(cfg.Feeds))
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("sample invalid: %v", err)
	}
	if cfg.PassiveUnreadOnly {
		t.Fatal("sample must ship passive_unread_only off")
	}
}

func TestAddFeedsNormalisesAndDeduplicates(t *testing.T) {
	cfg := Config{Feeds: []Feed{{URL: "https://a.example/rss", Title: "A", Category: "tech"}}}
	added := cfg.AddFeeds([]Feed{
		{URL: "  https://a.example/rss ", Title: "Duplicate"},
		{URL: "https://b.example/rss", Title: "  B  ", Category: "  News "},
		{URL: "https://c.example/rss"},
		{URL: "  "},
	})
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	if len(cfg.Feeds) != 3 {
		t.Fatalf("feeds = %d, want 3", len(cfg.Feeds))
	}
	if cfg.Feeds[1].Title != "B" || cfg.Feeds[1].Category != "news" {
		t.Fatalf("normalised feed = %+v", cfg.Feeds[1])
	}
	if cfg.Feeds[2].Category != "general" {
		t.Fatalf("default category = %q", cfg.Feeds[2].Category)
	}
	if got := cfg.AddFeeds(nil); got != 0 {
		t.Fatalf("empty merge added %d", got)
	}
}

func TestSavePreservingComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `# keep this header
refresh_interval: 10m # poll gap
theme: nord

# subscriptions
feeds:
  - url: https://example.com/rss
    title: Example
    category: tech
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Theme = "dracula"
	cfg.RefreshInterval = Duration(30 * time.Minute)
	cfg.Notifications = true
	cfg.PassiveItems = 50
	if err := SavePreservingComments(path, cfg); err != nil {
		t.Fatalf("SavePreservingComments: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	text := string(saved)
	for _, want := range []string{
		"# keep this header",
		"# subscriptions",
		"refresh_interval: 30m # poll gap",
		"theme: dracula",
		"notifications: true",
		"passive_items: 50",
		"- url: https://example.com/rss",
		"    title: Example",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "notifications:") > strings.Index(text, "feeds:") {
		t.Fatalf("a missing key was appended after the feeds:\n%s", text)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !strings.Contains(string(backup), "theme: nord") {
		t.Fatalf("backup is not the previous file:\n%s", backup)
	}

	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Theme != "dracula" || reloaded.RefreshInterval.Std() != 30*time.Minute {
		t.Fatalf("reloaded values = %q/%s", reloaded.Theme, reloaded.RefreshInterval)
	}
	if !reloaded.Notifications || reloaded.PassiveItems != 50 {
		t.Fatalf("reloaded flags = %v/%d", reloaded.Notifications, reloaded.PassiveItems)
	}
	if len(reloaded.Feeds) != 1 || reloaded.Feeds[0].Title != "Example" {
		t.Fatalf("feeds changed: %+v", reloaded.Feeds)
	}
}

func TestSavePreservingCommentsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Theme = "nord"
	if err := SavePreservingComments(path, cfg); err != nil {
		t.Fatalf("SavePreservingComments: %v", err)
	}
	reloaded, created, err := Load(path)
	if err != nil || created {
		t.Fatalf("reload: created=%v err=%v", created, err)
	}
	if reloaded.Theme != "nord" {
		t.Fatalf("theme = %q", reloaded.Theme)
	}
}

func TestSavePreservingCommentsKeepsFileMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("theme: nord\nfeeds:\n  - url: https://example.com/rss\n"), mode); err != nil {
				t.Fatalf("seed: %v", err)
			}
			cfg, _, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			cfg.Theme = "dracula"
			if err := SavePreservingComments(path, cfg); err != nil {
				t.Fatalf("SavePreservingComments: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if perm := info.Mode().Perm(); perm != mode {
				t.Fatalf("config mode = %o, want %o", perm, mode)
			}
			backup, err := os.Stat(path + ".bak")
			if err != nil {
				t.Fatalf("stat backup: %v", err)
			}
			if perm := backup.Mode().Perm(); perm != 0o600 {
				t.Fatalf("backup mode = %o, want 600", perm)
			}
		})
	}
}

func TestSaveCreatesPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove sample: %v", err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, created, err := Load(path); err != nil || created {
		t.Fatalf("saved config unreadable: created=%v err=%v", created, err)
	}
	file, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := file.Mode().Perm(); perm != 0o600 {
		t.Fatalf("new config mode = %o, want 600", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Fatalf("config dir mode = %o, want 700", perm)
	}
}

func TestSaveWritesBackupAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, created, err := Load(path)
	if err != nil || !created {
		t.Fatalf("Load: created=%v err=%v", created, err)
	}
	cfg.PassiveUnreadOnly = true
	cfg.Feeds = append(cfg.Feeds, Feed{URL: "https://new.example/rss", Title: "New", Category: "tech"})
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !strings.Contains(string(backup), "passive_unread_only: false") {
		t.Fatalf("backup is not the previous file:\n%s", backup)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.PassiveUnreadOnly {
		t.Fatal("passive_unread_only did not survive the round trip")
	}
	if len(reloaded.Feeds) != len(cfg.Feeds) {
		t.Fatalf("feeds = %d, want %d", len(reloaded.Feeds), len(cfg.Feeds))
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if strings.Contains(string(saved), "10m0s") {
		t.Fatalf("duration saved in Go's long form:\n%s", saved)
	}
	if !strings.Contains(string(saved), "\n  - url: ") {
		t.Fatalf("feeds are not indented by two spaces:\n%s", saved)
	}
	if err := Save("", cfg); err == nil {
		t.Fatal("Save accepted an empty path")
	}
}
