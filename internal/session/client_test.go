package session

import (
	"bytes"
	"testing"
)

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

func TestTermResetSeqDisablesKeyboardEnhancements(t *testing.T) {
	if !bytes.Contains([]byte(termResetSeq), []byte("\x1b[>4m")) {
		t.Fatalf("termResetSeq missing modifyOtherKeys reset: %q", termResetSeq)
	}
	if !bytes.Contains([]byte(termResetSeq), []byte("\x1b[=0;1u")) {
		t.Fatalf("termResetSeq missing kitty keyboard reset: %q", termResetSeq)
	}
	if !bytes.Contains([]byte(termResetSeq), []byte("\x1b[0 q")) {
		t.Fatalf("termResetSeq missing cursor-style reset: %q", termResetSeq)
	}
}

func TestExitSeqForSwitchSkipsFullClear(t *testing.T) {
	if got := exitSeqForSwitch(true); got != termResetSeq {
		t.Fatalf("exitSeqForSwitch(true) = %q, want termResetSeq", got)
	}
	if got := exitSeqForSwitch(false); got != termExitSeq {
		t.Fatalf("exitSeqForSwitch(false) = %q, want termExitSeq", got)
	}
}
