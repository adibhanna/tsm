package mux

import "testing"

func TestDirectionString(t *testing.T) {
	tests := []struct {
		dir  Direction
		want string
	}{
		{DirLeft, "left"},
		{DirRight, "right"},
		{DirUp, "up"},
		{DirDown, "down"},
		{Direction(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.dir.String(); got != tt.want {
			t.Errorf("Direction(%d).String() = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestParseDirection(t *testing.T) {
	tests := []struct {
		input string
		want  Direction
		ok    bool
	}{
		{"left", DirLeft, true},
		{"l", DirLeft, true},
		{"right", DirRight, true},
		{"r", DirRight, true},
		{"up", DirUp, true},
		{"u", DirUp, true},
		{"down", DirDown, true},
		{"d", DirDown, true},
		{"", 0, false},
		{"diagonal", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseDirection(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseDirection(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
