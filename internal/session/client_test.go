package session

import "testing"

func TestIsDetachKey(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "ctrl-backslash", data: []byte{0x1c}, want: true},
		{name: "kitty", data: []byte("\x1b[92;5u"), want: true},
		{name: "kitty-extended", data: []byte("\x1b[92;5:1u"), want: true},
		{name: "plain-text", data: []byte("hello"), want: false},
	}

	for _, tt := range tests {
		if got := isDetachKey(tt.data); got != tt.want {
			t.Fatalf("%s: isDetachKey(%q) = %v, want %v", tt.name, tt.data, got, tt.want)
		}
	}
}
