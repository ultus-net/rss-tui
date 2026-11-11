// Package config loads rss-tui settings from a YAML file and provides
// XDG-correct paths for the config file and state database.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRefreshInterval    = 10 * time.Minute
	DefaultPerFeedTimeout     = 20 * time.Second
	DefaultConcurrentFetches  = 6
	DefaultMaxArticlesPerFeed = 100
	DefaultPassiveItems       = 20
	DefaultTheme              = "catppuccin-mocha"
	defaultCategory           = "general"
)

// Duration is a time.Duration that decodes from Go duration strings in YAML.
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err == nil {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("invalid duration %q (use 30s, 10m, 2h)", raw)
		}
		*d = Duration(parsed)
		return nil
	}
	var seconds int64
	if err := node.Decode(&seconds); err != nil {
		return errors.New(`duration must be a string like "10m"`)
	}
	*d = Duration(time.Duration(seconds) * time.Second)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	dur := time.Duration(d)
	switch {
	case dur%time.Hour == 0:
		return fmt.Sprintf("%dh", int(dur.Hours())), nil
	case dur%time.Minute == 0:
		return fmt.Sprintf("%dm", int(dur.Minutes())), nil
	default:
		return dur.String(), nil
	}
}

// Feed is one subscription.
type Feed struct {
	URL      string `yaml:"url"`
	Title    string `yaml:"title,omitempty"`
	Category string `yaml:"category,omitempty"`
}

// DisplayName is the configured title, falling back to the feed host.
func (f Feed) DisplayName() string {
	if t := strings.TrimSpace(f.Title); t != "" {
		return t
	}
	if u, err := url.Parse(f.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return f.URL
}

// Config is the on-disk configuration.
type Config struct {
	RefreshInterval    Duration `yaml:"refresh_interval"`
	PerFeedTimeout     Duration `yaml:"per_feed_timeout"`
	ConcurrentFetches  int      `yaml:"concurrent_fetches"`
	MaxArticlesPerFeed int      `yaml:"max_articles_per_feed"`
	PassiveItems       int      `yaml:"passive_items"`
	PassiveUnreadOnly  bool     `yaml:"passive_unread_only"`
	Theme              string   `yaml:"theme"`
	Browser            string   `yaml:"browser"`
	Notifications      bool     `yaml:"notifications"`
	Feeds              []Feed   `yaml:"feeds"`
}

// Categories returns the distinct categories in configuration order.
func (c *Config) Categories() []string {
	seen := make(map[string]bool, len(c.Feeds))
	out := make([]string, 0, len(c.Feeds))
	for _, f := range c.Feeds {
		if seen[f.Category] {
			continue
		}
		seen[f.Category] = true
		out = append(out, f.Category)
	}
	return out
}

// URLs returns the configured feed URLs.
func (c *Config) URLs() []string {
	out := make([]string, 0, len(c.Feeds))
	for _, f := range c.Feeds {
		out = append(out, f.URL)
	}
	return out
}

// AddFeeds merges subscriptions into the config using the same normalisation
// Load applies. A URL that is already configured is skipped. It reports how
// many feeds were added.
func (c *Config) AddFeeds(extra []Feed) int {
	seen := make(map[string]bool, len(c.Feeds))
	for _, f := range c.Feeds {
		seen[f.URL] = true
	}
	added := 0
	for _, f := range extra {
		f.URL = strings.TrimSpace(f.URL)
		f.Title = strings.TrimSpace(f.Title)
		f.Category = strings.ToLower(strings.TrimSpace(f.Category))
		if f.Category == "" {
			f.Category = defaultCategory
		}
		if f.URL == "" || seen[f.URL] {
			continue
		}
		seen[f.URL] = true
		c.Feeds = append(c.Feeds, f)
		added++
	}
	return added
}

// Save writes cfg to path and keeps the previous file at path.bak. The write is
// atomic so an interrupted save cannot truncate the live config.
func Save(path string, cfg *Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config: empty path")
	}
	data, err := marshalConfig(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// SavePreservingComments writes cfg into the existing YAML file at path without
// discarding comments. Scalar settings are replaced in place and missing keys
// are appended before the feeds list; the feeds themselves stay exactly as
// authored. A missing file is created from cfg. The previous file is kept at
// path.bak.
func SavePreservingComments(path string, cfg *Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config: empty path")
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Save(path, cfg)
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(existing, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config: %s is not a YAML mapping", path)
	}
	root := doc.Content[0]

	encoded, err := marshalNode(cfg)
	if err != nil {
		return err
	}
	fresh := encoded.Content[0]
	freshValues := make(map[string]*yaml.Node, len(fresh.Content)/2)
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		freshValues[fresh.Content[i].Value] = fresh.Content[i+1]
	}

	present := make(map[string]bool, len(root.Content)/2)
	feedsAt := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		present[key] = true
		if key == "feeds" {
			feedsAt = i
			continue
		}
		replacement, ok := freshValues[key]
		if !ok {
			continue
		}
		// Keep any comment that was attached to the old value node.
		old := root.Content[i+1]
		replacement.HeadComment = old.HeadComment
		replacement.LineComment = old.LineComment
		replacement.FootComment = old.FootComment
		root.Content[i+1] = replacement
	}

	at := len(root.Content)
	if feedsAt >= 0 {
		at = feedsAt
	}
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		key := fresh.Content[i].Value
		if key == "feeds" || present[key] {
			continue
		}
		pair := []*yaml.Node{fresh.Content[i], fresh.Content[i+1]}
		root.Content = append(root.Content, nil, nil)
		copy(root.Content[at+2:], root.Content[at:])
		copy(root.Content[at:], pair)
		at += 2
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return writeAtomic(path, buf.Bytes())
}

func marshalConfig(cfg *Config) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buf.Bytes(), nil
}

func marshalNode(cfg *Config) (*yaml.Node, error) {
	data, err := marshalConfig(cfg)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse encoded config: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config: encoded document is not a mapping")
	}
	return &doc, nil
}

// writeAtomic backs up the existing file, then replaces it through a temporary
// file so an interrupted save cannot truncate the live config. The replacement
// keeps the file's permissions, defaulting to 0600 for a new file.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o600)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return fmt.Errorf("back up config: %w", err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read config for backup: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	// A unique temporary file keeps two concurrent saves from sharing one path.
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	tmp := file.Name()
	if err := file.Chmod(mode); err != nil {
		file.Close()
		os.Remove(tmp)
		return fmt.Errorf("write config: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tmp)
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// DefaultPath is $XDG_CONFIG_HOME/rss-tui/config.yaml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "rss-tui", "config.yaml"), nil
}

// DataDir is $XDG_DATA_HOME/rss-tui, falling back to ~/.local/share/rss-tui.
func DataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "rss-tui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "rss-tui"), nil
}

// Load reads path. When the file is missing it writes the sample config and
// returns it with created=true.
func Load(path string) (cfg *Config, created bool, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path, err = DefaultPath()
		if err != nil {
			return nil, false, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, parseErr := parse([]byte(sampleYAML))
		if parseErr != nil {
			return nil, false, fmt.Errorf("sample config is invalid: %w", parseErr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
			return nil, false, fmt.Errorf("create config dir: %w", mkErr)
		}
		if writeErr := os.WriteFile(path, []byte(sampleYAML), 0o644); writeErr != nil {
			return nil, false, fmt.Errorf("write sample config: %w", writeErr)
		}
		return cfg, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	cfg, err = parse(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, false, nil
}

func parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.RefreshInterval <= 0 {
		c.RefreshInterval = Duration(DefaultRefreshInterval)
	}
	if c.PerFeedTimeout <= 0 {
		c.PerFeedTimeout = Duration(DefaultPerFeedTimeout)
	}
	if c.ConcurrentFetches <= 0 {
		c.ConcurrentFetches = DefaultConcurrentFetches
	}
	if c.MaxArticlesPerFeed <= 0 {
		c.MaxArticlesPerFeed = DefaultMaxArticlesPerFeed
	}
	if c.PassiveItems <= 0 {
		c.PassiveItems = DefaultPassiveItems
	}
	if strings.TrimSpace(c.Theme) == "" {
		c.Theme = DefaultTheme
	}
	c.Theme = strings.ToLower(strings.TrimSpace(c.Theme))
	seen := make(map[string]bool, len(c.Feeds))
	feeds := c.Feeds[:0]
	for _, f := range c.Feeds {
		f.URL = strings.TrimSpace(f.URL)
		f.Title = strings.TrimSpace(f.Title)
		f.Category = strings.ToLower(strings.TrimSpace(f.Category))
		if f.Category == "" {
			f.Category = defaultCategory
		}
		if f.URL == "" || seen[f.URL] {
			continue
		}
		seen[f.URL] = true
		feeds = append(feeds, f)
	}
	c.Feeds = feeds
}

// Validate rejects settings the rest of the program cannot honour.
func (c Config) Validate() error {
	if d := c.RefreshInterval.Std(); d < 30*time.Second || d > 24*time.Hour {
		return fmt.Errorf("refresh_interval must be between 30s and 24h, got %s", d)
	}
	if d := c.PerFeedTimeout.Std(); d < time.Second || d > 5*time.Minute {
		return fmt.Errorf("per_feed_timeout must be between 1s and 5m, got %s", d)
	}
	if c.ConcurrentFetches < 1 || c.ConcurrentFetches > 32 {
		return fmt.Errorf("concurrent_fetches must be between 1 and 32, got %d", c.ConcurrentFetches)
	}
	if c.MaxArticlesPerFeed < 1 || c.MaxArticlesPerFeed > 1000 {
		return fmt.Errorf("max_articles_per_feed must be between 1 and 1000, got %d", c.MaxArticlesPerFeed)
	}
	if c.PassiveItems < 1 || c.PassiveItems > 200 {
		return fmt.Errorf("passive_items must be between 1 and 200, got %d", c.PassiveItems)
	}
	if len(c.Feeds) == 0 {
		return errors.New("no feeds configured")
	}
	for i, f := range c.Feeds {
		u, err := url.Parse(f.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("feeds[%d]: %q is not an absolute http(s) URL", i, f.URL)
		}
	}
	return nil
}

const sampleYAML = `# rss-tui configuration
#
# This file is read at startup. Deleting it regenerates this sample.
# State (read/unread, cached articles, chosen theme) lives in
# ~/.local/share/rss-tui/state.db and survives config edits.
#
# Durations use Go syntax: 30s, 10m, 2h.

refresh_interval: 10m
per_feed_timeout: 20s
concurrent_fetches: 6

# Articles kept per feed in the local cache.
max_articles_per_feed: 100

# Headlines shown by --passive.
passive_items: 20

# Show only unread headlines in --passive. Off by default.
passive_unread_only: false

# catppuccin-mocha | dracula | nord | gruvbox-dark | tokyo-night | solarized-dark
# Press "t" in the TUI to cycle themes; the choice is remembered across restarts.
theme: catppuccin-mocha

# Empty uses the system opener (xdg-open on Linux, open on macOS).
# Set a terminal browser if you prefer: "w3m", "lynx", "elinks".
browser: ""

# Desktop notification when a poll finds new articles. Off by default.
notifications: false

feeds:
  - url: https://news.ycombinator.com/rss
    title: Hacker News
    category: tech

  - url: https://lobste.rs/rss
    title: Lobsters
    category: tech

  - url: https://feeds.arstechnica.com/arstechnica/index
    title: Ars Technica
    category: tech

  - url: https://feeds.bbci.co.uk/news/world/rss.xml
    title: BBC World
    category: news
`
