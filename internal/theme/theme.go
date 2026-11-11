// Package theme holds the colour palettes. Every palette fills the same
// semantic roles, so adding an IDE theme is one struct entry, not a UI change.
package theme

import (
	"regexp"
	"strings"
)

// Palette is one colour scheme. All values are #rrggbb strings.
type Palette struct {
	Name        string
	Label       string
	Fg          string // article titles and body
	Muted       string // read titles, timestamps
	Faint       string // hints, separators, read dots
	Accent      string // unread dots, active border, pane title
	AccentAlt   string // source tags and secondary metadata
	Border      string
	BorderFocus string
	SelectionBg string
	SelectionFg string
	StatusBg    string
	StatusFg    string
	Error       string
	Warning     string
	Search      string
}

const DefaultName = "catppuccin-mocha"

var order = []string{
	"catppuccin-mocha",
	"dracula",
	"nord",
	"gruvbox-dark",
	"tokyo-night",
	"solarized-dark",
}

var palettes = map[string]Palette{
	"catppuccin-mocha": {
		Name: "catppuccin-mocha", Label: "Catppuccin Mocha",
		Fg: "#cdd6f4", Muted: "#a6adc8", Faint: "#6c7086",
		Accent: "#cba6f7", AccentAlt: "#89b4fa",
		Border: "#45475a", BorderFocus: "#cba6f7",
		SelectionBg: "#313244", SelectionFg: "#f5e0dc",
		StatusBg: "#181825", StatusFg: "#9399b2",
		Error: "#f38ba8", Warning: "#f9e2af", Search: "#a6e3a1",
	},
	"dracula": {
		Name: "dracula", Label: "Dracula",
		Fg: "#f8f8f2", Muted: "#a3a9c9", Faint: "#6272a4",
		Accent: "#bd93f9", AccentAlt: "#8be9fd",
		Border: "#44475a", BorderFocus: "#bd93f9",
		SelectionBg: "#44475a", SelectionFg: "#f8f8f2",
		StatusBg: "#21222c", StatusFg: "#9aa4d2",
		Error: "#ff5555", Warning: "#f1fa8c", Search: "#50fa7b",
	},
	"nord": {
		Name: "nord", Label: "Nord",
		Fg: "#d8dee9", Muted: "#a5b1c9", Faint: "#616e88",
		Accent: "#88c0d0", AccentAlt: "#81a1c1",
		Border: "#434c5e", BorderFocus: "#88c0d0",
		SelectionBg: "#3b4252", SelectionFg: "#eceff4",
		StatusBg: "#242933", StatusFg: "#8f9bb3",
		Error: "#bf616a", Warning: "#ebcb8b", Search: "#a3be8c",
	},
	"gruvbox-dark": {
		Name: "gruvbox-dark", Label: "Gruvbox Dark",
		Fg: "#ebdbb2", Muted: "#bdae93", Faint: "#928374",
		Accent: "#fabd2f", AccentAlt: "#83a598",
		Border: "#504945", BorderFocus: "#fabd2f",
		SelectionBg: "#3c3836", SelectionFg: "#fbf1c7",
		StatusBg: "#1d2021", StatusFg: "#a89984",
		Error: "#fb4934", Warning: "#d79921", Search: "#b8bb26",
	},
	"tokyo-night": {
		Name: "tokyo-night", Label: "Tokyo Night",
		Fg: "#c0caf5", Muted: "#9aa5ce", Faint: "#565f89",
		Accent: "#7aa2f7", AccentAlt: "#bb9af7",
		Border: "#2f334d", BorderFocus: "#7aa2f7",
		SelectionBg: "#2a2e45", SelectionFg: "#c8d3f5",
		StatusBg: "#16161e", StatusFg: "#737aa2",
		Error: "#f7768e", Warning: "#e0af68", Search: "#9ece6a",
	},
	"solarized-dark": {
		Name: "solarized-dark", Label: "Solarized Dark",
		Fg: "#93a1a1", Muted: "#839496", Faint: "#586e75",
		Accent: "#268bd2", AccentAlt: "#2aa198",
		Border: "#073642", BorderFocus: "#268bd2",
		SelectionBg: "#073642", SelectionFg: "#eee8d5",
		StatusBg: "#001f27", StatusFg: "#657b83",
		Error: "#dc322f", Warning: "#b58900", Search: "#859900",
	},
}

var aliases = map[string]string{
	"catppuccin":       "catppuccin-mocha",
	"catppuccin-mocha": "catppuccin-mocha",
	"mocha":            "catppuccin-mocha",
	"dracula":          "dracula",
	"nord":             "nord",
	"gruvbox":          "gruvbox-dark",
	"gruvbox-dark":     "gruvbox-dark",
	"tokyo":            "tokyo-night",
	"tokyo-night":      "tokyo-night",
	"solarized":        "solarized-dark",
	"solarized-dark":   "solarized-dark",
}

var hexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Resolve returns the palette for name and whether the name was recognised.
// Unknown names fall back to the default palette.
func Resolve(name string) (Palette, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := aliases[key]; ok {
		return palettes[canonical], true
	}
	return palettes[DefaultName], false
}

// Get is Resolve without the recognition flag.
func Get(name string) Palette {
	p, _ := Resolve(name)
	return p
}

// Names lists palette names in cycle order.
func Names() []string {
	out := make([]string, len(order))
	copy(out, order)
	return out
}

// Next returns the palette after name, wrapping at the end.
func Next(name string) Palette {
	p, _ := Resolve(name)
	for i, n := range order {
		if n == p.Name {
			return palettes[order[(i+1)%len(order)]]
		}
	}
	return palettes[order[0]]
}

// Valid reports whether every role holds a hex colour. The test suite runs this
// over all palettes so a typo cannot ship.
func (p Palette) Valid() bool {
	values := []string{
		p.Fg, p.Muted, p.Faint, p.Accent, p.AccentAlt, p.Border, p.BorderFocus,
		p.SelectionBg, p.SelectionFg, p.StatusBg, p.StatusFg, p.Error, p.Warning, p.Search,
	}
	for _, v := range values {
		if !hexRE.MatchString(v) {
			return false
		}
	}
	return true
}
