package rust

import (
	"strings"
	"testing"
)

// Constructor availability, not the current union tag or a nonzero-default
// shortcut, decides whether a fresh payload starts at new() or default().
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
	_, read, found := strings.Cut(string(files["arminit.rs"]), "pub fn read_choice(")
	if !found {
		t.Fatal("generated Rust has no read_choice")
	}
	read, _, found = strings.Cut(read, "\n}\n")
	if !found {
		t.Fatal("generated read_choice is unterminated")
	}
	for _, want := range []string{
		"1 => {\n            let mut arm = Explicit::new();\n            read_explicit(stream, &mut arm)?;",
		"2 => {\n            let mut arm = Nested::new();\n            read_nested(stream, &mut arm)?;",
		"3 => {\n            let mut arm = Counted::new();\n            read_counted(stream, &mut arm)?;",
		"4 => {\n            let mut arm = Plain::default();\n            read_plain(stream, &mut arm)?;",
		"5 => {\n            let mut arm = ExplicitZero::new();\n            read_explicit_zero(stream, &mut arm)?;",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("each tag must reconstruct its payload before reading, without a same-tag exception: want %q in:\n%s", want, read)
		}
	}
}
