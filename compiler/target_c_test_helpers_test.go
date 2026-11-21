package compiler

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateCUnqualified is the C emitters' output in the schema's own
// spellings, before internal/codegen/cqualify gives every name its package.
// The emitter-shape tests read this form: what a test pins there is WHAT is
// emitted, and the package spelling is cqualify's own tests' business.
func generateCUnqualified(u *ir.Unit) (map[string][]byte, error) {
	return emitC(u, nil)
}

// cCarries reports whether emitted C carries needle, read as TOKENS: the C
// table emitter's layout is the house layout pass's (internal/codegen/cfmt)
// and moves with .clang-format, so a needle is matched with all whitespace
// taken out of both sides. Presence AND absence checks on emitted C go
// through it: an absence check spelled in a layout the emitter no longer
// writes would pass on anything.
func cCarries(text, needle string) bool {
	squash := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\n', '\r':
				return -1
			}
			return r
		}, s)
	}
	return strings.Contains(squash(text), squash(needle))
}
