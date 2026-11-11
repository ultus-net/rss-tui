package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ultus-net/rss-tui/internal/config"
	"github.com/ultus-net/rss-tui/internal/store"
)

func TestAdoptLegacyThemeMovesMetaIntoYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetMeta(ctx, "theme", "gruvbox"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	adopted, ok := adoptLegacyTheme(st, cfg, path)
	if !ok || adopted != "gruvbox-dark" {
		t.Fatalf("adopted = %q/%v", adopted, ok)
	}
	if cfg.Theme != "gruvbox-dark" {
		t.Fatalf("cfg.Theme = %q", cfg.Theme)
	}
	reloaded, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Theme != "gruvbox-dark" {
		t.Fatalf("YAML theme = %q", reloaded.Theme)
	}
	if _, ok, err := st.Meta(ctx, "theme"); err != nil || ok {
		t.Fatalf("stale meta survived: ok=%v err=%v", ok, err)
	}
	if _, ok := adoptLegacyTheme(st, cfg, path); ok {
		t.Fatal("migration ran a second time")
	}
}

func TestAdoptLegacyThemeDropsUnknownName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetMeta(ctx, "theme", "not-a-theme"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	if adopted, ok := adoptLegacyTheme(st, cfg, path); ok || adopted != "" {
		t.Fatalf("adopted = %q/%v", adopted, ok)
	}
	if cfg.Theme != config.DefaultTheme {
		t.Fatalf("cfg.Theme = %q", cfg.Theme)
	}
	if _, ok, err := st.Meta(ctx, "theme"); err != nil || ok {
		t.Fatalf("stale meta survived: ok=%v err=%v", ok, err)
	}
}

func TestAdoptLegacyThemeLeavesChosenYAMLTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Theme = "nord"
	if err := config.SavePreservingComments(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetMeta(ctx, "theme", "dracula"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	if adopted, ok := adoptLegacyTheme(st, cfg, path); ok || adopted != "" {
		t.Fatalf("adopted = %q/%v", adopted, ok)
	}
	if cfg.Theme != "nord" {
		t.Fatalf("cfg.Theme = %q", cfg.Theme)
	}
	reloaded, _, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Theme != "nord" {
		t.Fatalf("YAML theme = %q", reloaded.Theme)
	}
	if _, ok, err := st.Meta(ctx, "theme"); err != nil || ok {
		t.Fatalf("stale meta survived: ok=%v err=%v", ok, err)
	}
}
