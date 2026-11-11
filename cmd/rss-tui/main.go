// Command rss-tui is a fast terminal RSS/Atom reader with an interactive
// Bubble Tea UI and a passive ticker mode for tmux panes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/feed"
	"github.com/ultus-net/rss-tui/internal/notify"
	"github.com/ultus-net/rss-tui/internal/opml"
	"github.com/ultus-net/rss-tui/internal/store"
	"github.com/ultus-net/rss-tui/internal/theme"
	"github.com/ultus-net/rss-tui/internal/ui"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rss-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export-opml":
			return runExportOPML(os.Args[2:])
		case "import-opml":
			return runImportOPML(os.Args[2:])
		case "help":
			printUsage()
			return nil
		}
	}
	fs := flag.NewFlagSet("rss-tui", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.yaml (default ~/.config/rss-tui/config.yaml)")
	passive := fs.Bool("passive", false, "non-interactive live ticker mode")
	passiveShort := fs.Bool("p", false, "alias for --passive")
	once := fs.Bool("once", false, "fetch once, print the feed, and exit")
	themeFlag := fs.String("theme", "", "override the configured colour theme")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("rss-tui " + version)
		return nil
	}

	path, err := resolveConfigPath(*configPath)
	if err != nil {
		return err
	}
	cfg, created, err := config.Load(path)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(os.Stderr, "rss-tui: created sample config at %s\n", path)
	}

	themeName := cfg.Theme
	explicit := false
	if strings.TrimSpace(*themeFlag) != "" {
		themeName = *themeFlag
		explicit = true
	}

	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	// Older versions stored the theme in SQLite. Reconcile it into the YAML once
	// so the config file is the single source of truth, then drop the stale key.
	if adopted, ok := adoptLegacyTheme(st, cfg, path); ok && !explicit {
		themeName = adopted
	}

	palette, known := theme.Resolve(themeName)
	if !known {
		if explicit {
			return fmt.Errorf("unknown theme %q (available: %s)", themeName, strings.Join(theme.Names(), ", "))
		}
		fmt.Fprintf(os.Stderr, "rss-tui: unknown theme %q, using %s\n", themeName, palette.Name)
	}

	// The config file is the source of truth for subscriptions.
	if err := st.PruneFeeds(context.Background(), cfg.URLs()); err != nil {
		fmt.Fprintf(os.Stderr, "rss-tui: %v\n", err)
	}

	deps := ui.Deps{
		Cfg:        cfg,
		Store:      st,
		Fetcher:    feed.New(feed.Options{Timeout: cfg.PerFeedTimeout.Std(), Workers: cfg.ConcurrentFetches}),
		Notifier:   notify.New(cfg.Notifications),
		Theme:      palette,
		Browser:    cfg.Browser,
		ConfigPath: path,
	}

	if *passive || *passiveShort || *once {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return ui.RunPassive(ctx, deps, ui.PassiveOptions{Once: *once, Out: os.Stdout, Color: true})
	}

	if os.Getenv("NO_COLOR") != "" || !term.IsTerminal(int(os.Stdout.Fd())) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	program := tea.NewProgram(ui.New(deps), tea.WithAltScreen())
	_, err = program.Run()
	return err
}

func resolveConfigPath(flagValue string) (string, error) {
	if p := strings.TrimSpace(flagValue); p != "" {
		return p, nil
	}
	return config.DefaultPath()
}

// adoptLegacyTheme reconciles a theme left in the SQLite meta table by an older
// version. While the YAML still holds the default it adopts the stored theme
// into cfg and saves it. Either way the stale key is deleted, so the YAML value
// owns the choice from the next start.
func adoptLegacyTheme(st *store.Store, cfg *config.Config, path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stored, ok, err := st.Meta(ctx, "theme")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rss-tui: read stored theme: %v\n", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	adopted := ""
	p, known := theme.Resolve(stored)
	if known && cfg.Theme == config.DefaultTheme {
		cfg.Theme = p.Name
		if err := config.SavePreservingComments(path, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "rss-tui: migrate theme: %v\n", err)
			return "", false
		}
		adopted = p.Name
	}
	if err := st.DeleteMeta(ctx, "theme"); err != nil {
		fmt.Fprintf(os.Stderr, "rss-tui: drop stored theme: %v\n", err)
	}
	return adopted, adopted != ""
}

func printUsage() {
	fmt.Fprintln(os.Stdout, `usage: rss-tui [flags]
       rss-tui export-opml [--config path] [-o file]
       rss-tui import-opml [--config path] [--replace] file.opml

Flags for the interactive and passive modes:
  --config path   config file (default ~/.config/rss-tui/config.yaml)
  --passive, -p   live ticker mode for a tmux pane
  --once          fetch once, print the feed, and exit
  --theme name    override the configured colour theme
  --version       print the version`)
}

func runExportOPML(args []string) error {
	fs := flag.NewFlagSet("rss-tui export-opml", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.yaml (default ~/.config/rss-tui/config.yaml)")
	out := fs.String("o", "", "write to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := resolveConfigPath(*configPath)
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		return err
	}
	feeds := make([]opml.Feed, 0, len(cfg.Feeds))
	for _, f := range cfg.Feeds {
		feeds = append(feeds, opml.Feed{Title: f.DisplayName(), XMLURL: f.URL, Category: f.Category})
	}
	if strings.TrimSpace(*out) == "" {
		return opml.Export(os.Stdout, feeds)
	}
	file, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	if err := opml.Export(file, feeds); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", *out, err)
	}
	fmt.Fprintf(os.Stderr, "rss-tui: wrote %d feeds to %s\n", len(feeds), *out)
	return nil
}

func runImportOPML(args []string) error {
	fs := flag.NewFlagSet("rss-tui import-opml", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.yaml (default ~/.config/rss-tui/config.yaml)")
	replace := fs.Bool("replace", false, "replace the configured feeds instead of merging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: rss-tui import-opml [--config path] [--replace] file.opml")
	}
	source := fs.Arg(0)
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	parsed, parseErr := opml.Parse(file)
	file.Close()
	if parseErr != nil {
		return fmt.Errorf("%s: %w", source, parseErr)
	}
	if len(parsed) == 0 {
		return fmt.Errorf("%s: no subscriptions found", source)
	}
	path, err := resolveConfigPath(*configPath)
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		return err
	}
	if *replace {
		cfg.Feeds = nil
	}
	extra := make([]config.Feed, 0, len(parsed))
	for _, p := range parsed {
		extra = append(extra, config.Feed{URL: p.XMLURL, Title: p.Title, Category: p.Category})
	}
	added := cfg.AddFeeds(extra)
	if len(cfg.Feeds) == 0 {
		return errors.New("import would leave no feeds configured")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("rss-tui: %d subscriptions in %s: %d added, %d already configured\n",
		len(parsed), source, added, len(parsed)-added)
	return nil
}
