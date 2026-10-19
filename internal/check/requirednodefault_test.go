// A `| required` field whose range excludes zero takes no default (docs/SPEC-TABLES.md
// §2.3, SPEC §4.6): the required spelling checks clean, while the unrequired spelling
// keeps its "declare a default in range" diagnostic.
package check

import (
	"strings"
	"testing"
)

func TestRequiredFieldNeedsNoDefault(t *testing.T) {
	t.Parallel()
	t.Run("required spelling checks clean", func(t *testing.T) {
		checkUnit(t, `package t
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
`)
	})
	t.Run("unrequired spelling keeps the diagnostic", func(t *testing.T) {
		errs := runUnit(t, map[string]string{"T.schema": "package t\ntable Exact\n{\n    ver uint8 | min = 7, max = 7\n}\n"})
		if len(errs) == 0 {
			t.Fatal("compiled clean — non-required exact range did not demand a default")
		}
		var got bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "declare a default in range") {
				got = true
			}
		}
		if !got {
			t.Fatalf("the unrequired diagnostic is not present: %v", errs)
		}
	})
}
