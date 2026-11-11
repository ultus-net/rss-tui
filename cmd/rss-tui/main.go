// Command rss-tui is a fast terminal RSS/Atom reader with an interactive
// Bubble Tea UI and a passive ticker mode for tmux panes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/feed"
	"github.com/ultus-net/rss-tui/internal/notify"
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

	path := strings.TrimSpace(*configPath)
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			return err
		}
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
	palette, known := theme.Resolve(themeName)
	if !known {
		if explicit {
			return fmt.Errorf("unknown theme %q (available: %s)", themeName, strings.Join(theme.Names(), ", "))
		}
		fmt.Fprintf(os.Stderr, "rss-tui: unknown theme %q, using %s\n", themeName, palette.Name)
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
	// The config file is the source of truth for subscriptions.
	if err := st.PruneFeeds(context.Background(), cfg.URLs()); err != nil {
		fmt.Fprintf(os.Stderr, "rss-tui: %v\n", err)
	}

	deps := ui.Deps{
		Cfg:           cfg,
		Store:         st,
		Fetcher:       feed.New(feed.Options{Timeout: cfg.PerFeedTimeout.Std(), Workers: cfg.ConcurrentFetches}),
		Notifier:      notify.New(cfg.Notifications),
		Theme:         palette,
		ThemeExplicit: explicit,
		Browser:       cfg.Browser,
		ConfigPath:    path,
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
