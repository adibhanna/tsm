//go:build cgo

package session

/*
#cgo linux LDFLAGS: -ldl
#include <stddef.h>
#include <dlfcn.h>

static const char *const tsm_required_ghostty_symbols[] = {
    "ghostty_terminal_new",
    "ghostty_terminal_free",
    "ghostty_terminal_resize",
    "ghostty_terminal_vt_write",
    "ghostty_formatter_terminal_new",
    "ghostty_formatter_free",
    "ghostty_formatter_format_alloc",
};

// tsm_missing_ghostty_symbol returns the name of the first required
// libghostty-vt symbol that does not resolve at runtime, or NULL if every
// symbol is present. The returned pointer is a string literal owned by the
// binary and must not be freed.
static const char *tsm_missing_ghostty_symbol(void) {
    const size_t count = sizeof(tsm_required_ghostty_symbols) /
                         sizeof(tsm_required_ghostty_symbols[0]);
    for (size_t i = 0; i < count; i++) {
        dlerror();
        if (dlsym(RTLD_DEFAULT, tsm_required_ghostty_symbols[i]) == NULL) {
            return tsm_required_ghostty_symbols[i];
        }
    }
    return NULL;
}
*/
import "C"

// MissingGhosttySymbol probes the loaded libghostty-vt at runtime via dlsym
// and returns the name of the first required symbol that is not resolvable,
// or "" if every required symbol is present.
//
// pkg-config is only a build-time signal: distros can ship libghostty-vt.pc
// alongside a library that has drifted from the C ABI tsm was built against
// (e.g. Arch's ghostty 1.3.1 ships a libghostty-vt that does not export
// ghostty_terminal_new). This check catches that drift before the daemon
// crashes with an opaque "symbol lookup error".
func MissingGhosttySymbol() string {
	if missing := C.tsm_missing_ghostty_symbol(); missing != nil {
		return C.GoString(missing)
	}
	return ""
}
