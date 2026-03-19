//go:build cgo && !noghosttyvt

package session

func RestoreBackendName() string {
	return "libghostty-vt"
}
