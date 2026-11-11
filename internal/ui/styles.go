package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ultus-net/rss-tui/internal/theme"
)

// styles derives lipgloss styles from a palette. Every style that paints on the
// status bar or a selected row carries the background itself, because a nested
// style reset would otherwise punch a hole in the fill.
type styles struct {
	p theme.Palette
}

func newStyles(p theme.Palette) styles { return styles{p: p} }

func (s styles) pane(active bool) lipgloss.Style {
	border := s.p.Border
	if active {
		border = s.p.BorderFocus
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(border)).
		Padding(0, 1)
}

func (s styles) title() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Accent)).Bold(true)
}

func (s styles) body() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Fg))
}

func (s styles) header() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Muted))
}

func (s styles) meta() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Muted))
}

func (s styles) faint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Faint))
}

func (s styles) tag() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.AccentAlt))
}

func (s styles) search() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Search))
}

func (s styles) warning() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Warning))
}

func (s styles) failure() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Error))
}

func (s styles) statusBase() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(s.p.StatusBg)).
		Foreground(lipgloss.Color(s.p.StatusFg))
}

func (s styles) statusText(color string) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(s.p.StatusBg)).
		Foreground(lipgloss.Color(color))
}

func (s styles) statusBar() lipgloss.Style {
	return s.statusBase().Padding(0, 1)
}

func (s styles) rowFill(selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().Background(lipgloss.Color(s.p.SelectionBg))
	}
	return lipgloss.NewStyle()
}

func (s styles) rowTitle(read, selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(s.p.SelectionBg)).
			Foreground(lipgloss.Color(s.p.SelectionFg))
	}
	if read {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Muted))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.Fg))
}

func (s styles) rowMarker(selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(s.p.SelectionBg)).
			Foreground(lipgloss.Color(s.p.Accent))
	}
	return lipgloss.NewStyle()
}

func (s styles) rowDot(read, selected bool) lipgloss.Style {
	color := s.p.Accent
	if read {
		color = s.p.Faint
	}
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(s.p.SelectionBg)).
			Foreground(lipgloss.Color(color))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func (s styles) rowTag(selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(s.p.SelectionBg)).
			Foreground(lipgloss.Color(s.p.AccentAlt))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(s.p.AccentAlt))
}

func (s styles) helpBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(s.p.BorderFocus)).
		Padding(1, 2)
}
