//go:build !cgo || noghosttyvt

package session

func RestoreBackendName() string {
	return "mode-tracker"
}
