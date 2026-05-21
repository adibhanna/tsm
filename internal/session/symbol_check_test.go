package session

import "testing"

// TestMissingGhosttySymbolEmptyForWorkingBuild verifies the dlsym probe
// finds every required symbol in a build whose libghostty-vt actually
// exports them. This is the success case the doctor reports as "ok".
// On the stub (non-cgo) build the function is a no-op that returns "".
func TestMissingGhosttySymbolEmptyForWorkingBuild(t *testing.T) {
	if got := MissingGhosttySymbol(); got != "" {
		t.Fatalf("MissingGhosttySymbol() = %q, want empty (every required symbol should resolve in this build)", got)
	}
}
