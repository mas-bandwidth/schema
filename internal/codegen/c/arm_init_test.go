package c

import (
	"strings"
	"testing"
)

// The read must use exactly the constructors that the same backend emits:
// explicit (including zero), transitive and born-count defaults need new_*,
// while an ordinary payload must not call a nonexistent constructor.
func TestUnionReadUsesDeclaredInitialValues(t *testing.T) {
	u := unitFromSource(t, "ArmInit.schema", `package arminit
type Explicit { value uint8 = 7 }
type ExplicitZero { value uint8 = 0 }
type Plain { value uint8 }
type Nested { child Explicit }
type Counted { values [1..3]uint8 }
union Choice {
    specified Explicit
    nested Nested
    counted Counted
    plain Plain
    zero ExplicitZero
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["ArmInitWire.h"])
	_, read, found := strings.Cut(header, "int read_choice( ")
	if !found {
		t.Fatal("generated C has no read_choice")
	}
	read, _, found = strings.Cut(read, "\n}\n")
	if !found {
		t.Fatal("generated read_choice is unterminated")
	}
	for _, want := range []string{
		"case 1:\n            value->as.specified = new_explicit();\n            return read_explicit( stream, &value->as.specified );",
		"case 2:\n            value->as.nested = new_nested();\n            return read_nested( stream, &value->as.nested );",
		"case 3:\n            value->as.counted = new_counted();\n            return read_counted( stream, &value->as.counted );",
		"case 4:\n            memset( &value->as.plain, 0, sizeof( value->as.plain ) );\n            return read_plain( stream, &value->as.plain );",
		"case 5:\n            value->as.zero = new_explicit_zero();\n            return read_explicit_zero( stream, &value->as.zero );",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("each tag must reconstruct its payload before reading, without a same-tag exception: want %q in:\n%s", want, read)
		}
	}
	if strings.Contains(header, "new_plain(") {
		t.Error("an ordinary payload must not require a constructor")
	}
}
