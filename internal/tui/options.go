package tui

import (
	"fmt"
	"strings"
)

type Mode int

const (
	ModeFull Mode = iota
	ModeSimplified
)

func (m Mode) String() string {
	switch m {
	case ModeSimplified:
		return "simplified"
	default:
		return "full"
	}
}

func ParseMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "full":
		return ModeFull, nil
	case "simplified", "simple", "palette":
		return ModeSimplified, nil
	default:
		return ModeFull, fmt.Errorf("unknown TUI mode %q", raw)
	}
}

type Keymap int

const (
	KeymapUnset Keymap = iota
	KeymapDefault
	KeymapPalette
)

func (k Keymap) String() string {
	switch k {
	case KeymapPalette:
		return "palette"
	case KeymapDefault:
		return "default"
	default:
		return ""
	}
}

func ParseKeymap(raw string) (Keymap, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default":
		return KeymapDefault, nil
	case "palette":
		return KeymapPalette, nil
	default:
		return KeymapUnset, fmt.Errorf("unknown keymap %q", raw)
	}
}

type Options struct {
	Mode        Mode
	Keymap      Keymap
	ShowHelp    bool
	ShowHelpSet bool
	Bindings    Bindings
}

func NormalizeOptions(opts Options) Options {
	if opts.Mode != ModeFull && opts.Mode != ModeSimplified {
		opts.Mode = ModeFull
	}
	if opts.Keymap == KeymapUnset {
		opts.Keymap = KeymapDefault
	}
	if !opts.ShowHelpSet {
		opts.ShowHelp = true
	}
	if opts.Bindings.IsZero() {
		opts.Bindings = DefaultBindings(opts.Keymap)
	}
	return opts
}
