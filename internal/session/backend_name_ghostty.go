//go:build cgo

package session

func RestoreBackendName() string {
	return "libghostty-vt"
}
