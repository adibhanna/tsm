//go:build !cgo

package session

func RestoreBackendName() string {
	return "stub"
}
