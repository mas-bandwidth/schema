package darttable

import (
	"strings"
	"testing"
)

// A strict write refuses a deprecated enum variant (docs/SPEC-TABLES.md section 2.15):
// the Dart wire save and the fixed save both assert against it. The deprecated
// variant is Old, ordinal 2.
func TestDartWriteAssertsDeprecatedEnumVariant(t *testing.T) {
	u := unitFrom(t, `
package depr

enum Phase
{
    Live
    Old | deprecated
    Next
}

table VarRoot
{
    phase Phase
}

fixed table FixedRoot
{
    phase Phase
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var wire, fixed string
	for name, b := range files {
		switch {
		case strings.HasSuffix(name, "Wire.dart"):
			wire = string(b)
		case strings.HasSuffix(name, "Fixed.dart"):
			fixed = string(b)
		}
	}
	if !strings.Contains(wire, "assert(!(v.phase == 2));") {
		t.Fatalf("wire save does not refuse the deprecated variant")
	}
	if !strings.Contains(fixed, "== 2));") || !strings.Contains(fixed, "assert(!(") {
		t.Fatalf("fixed save does not refuse the deprecated variant")
	}
}
