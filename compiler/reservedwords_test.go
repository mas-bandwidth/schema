package compiler

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
)

// TestEveryPortRegistersWithItsReservedWords: every built-in generator and
// every carries registration (the build-tagged ports included) names a
// language with a row in the one reserved-word table (SPEC §4.6), and a
// target without one cannot register.
func TestEveryPortRegistersWithItsReservedWords(t *testing.T) {
	t.Parallel()
	for _, g := range builtins() {
		for _, name := range g.Names() {
			if _, ok := check.ReservedLanguageOf(name); !ok {
				t.Errorf("built-in target %q has no reserved words in the one table", name)
			}
		}
	}
	for target := range targetCarries {
		if _, ok := check.ReservedLanguageOf(target); !ok {
			t.Errorf("carries target %q has no reserved words in the one table", target)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a target with no reserved words must panic")
		}
	}()
	requireReservedWords("cobol")
}
