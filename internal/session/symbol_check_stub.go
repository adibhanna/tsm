//go:build !cgo

package session

// MissingGhosttySymbol is a no-op in stub (non-cgo) builds because the
// binary does not link libghostty-vt at all.
func MissingGhosttySymbol() string {
	return ""
}
