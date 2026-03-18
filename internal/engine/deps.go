package engine

import (
	"os/exec"

	"github.com/atotto/clipboard"
)

type runtimeDeps struct {
	command        func(name string, arg ...string) *exec.Cmd
	clipboardWrite func(text string) error
}

var deps = runtimeDeps{
	command:        exec.Command,
	clipboardWrite: clipboard.WriteAll,
}

// runCombinedOutput is used only for `ps` in process.go.
func runCombinedOutput(name string, arg ...string) ([]byte, error) {
	return deps.command(name, arg...).CombinedOutput()
}
