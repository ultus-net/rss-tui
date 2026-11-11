package theme

import (
	"strings"
	"testing"
)

func TestAllPalettesValid(t *testing.T) {
	names := Names()
	if len(names) != 6 {
		t.Fatalf("palettes = %d, want 6", len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		p, ok := Resolve(name)
		if !ok {
			t.Fatalf("%s not resolvable", name)
		}
		if seen[p.Name] {
			t.Fatalf("duplicate palette %s", p.Name)
		}
		seen[p.Name] = true
		if !p.Valid() {
			t.Fatalf("%s has an invalid hex colour: %+v", name, p)
		}
		if p.Label == "" {
			t.Fatalf("%s has no label", name)
		}
	}
}

func TestResolveAliasesAndFallback(t *testing.T) {
	cases := map[string]string{
		"catppuccin": "catppuccin-mocha",
		"mocha":      "catppuccin-mocha",
		"gruvbox":    "gruvbox-dark",
		"tokyo":      "tokyo-night",
		"solarized":  "solarized-dark",
		"nord":       "nord",
	}
	for input, want := range cases {
		p, ok := Resolve(strings.ToUpper(input))
		if !ok || p.Name != want {
			t.Fatalf("Resolve(%q) = %s/%v, want %s", input, p.Name, ok, want)
		}
	}
	p, ok := Resolve("does-not-exist")
	if ok {
		t.Fatal("unknown theme should report ok=false")
	}
	if p.Name != DefaultName {
		t.Fatalf("fallback = %s", p.Name)
	}
}

func TestNextCyclesInOrder(t *testing.T) {
	cur := DefaultName
	seen := map[string]bool{}
	for i := 0; i < len(Names()); i++ {
		p := Next(cur)
		if seen[p.Name] {
			t.Fatalf("cycle repeated %s", p.Name)
		}
		seen[p.Name] = true
		cur = p.Name
	}
	if cur != DefaultName {
		t.Fatalf("cycle ended at %s, want %s", cur, DefaultName)
	}
}

func TestStepWalksBothWays(t *testing.T) {
	names := Names()
	for i, name := range names {
		if got := Step(name, 1); got.Name != names[(i+1)%len(names)] {
			t.Fatalf("Step(%s, 1) = %s", name, got.Name)
		}
		if got := Step(name, -1); got.Name != names[(i-1+len(names))%len(names)] {
			t.Fatalf("Step(%s, -1) = %s", name, got.Name)
		}
	}
	if got := Step(DefaultName, len(names)); got.Name != DefaultName {
		t.Fatalf("full step = %s", got.Name)
	}
	if got := Step("nonsense", 1); got.Name != names[1] {
		t.Fatalf("unknown step = %s", got.Name)
	}
}
