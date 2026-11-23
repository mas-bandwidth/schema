package rusttable

// TestRustBoolElementConditionHasNoParens holds the generated Rust to its own
// lint: a bool array element is written (wire and JSON text) through the
// element binding `(*e)`, and `if (*e) { .. }` is rustc unused_parens, an
// error under the leg gates deny(warnings) (verify-leg (rust, tablegen),
// pin:fbool and pin:cbool). The condition is `*e`.
//
// A UNIT TEST: it generates source and reads it; no rustc, clock, socket or
// network.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

const boolElementSchema = `package pinbool

fixed table FixRoot {
    few [2]bool
}

table VarRoot {
    few [2]bool
    some [2..4]bool
}
`

func TestRustBoolElementConditionHasNoParens(t *testing.T) {
	t.Parallel()
	src := []byte(boolElementSchema)
	f, perrs := parser.Parse("Pin.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Pin.schema", Name: "Pin.schema", Base: "Pin", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(modules) == 0 {
		t.Fatal("Generate emitted no module")
	}
	elementWrites := 0
	for name, body := range modules {
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "if (*e) {") {
				t.Errorf("%s:%d: a parenthesised if condition (rustc unused_parens): %s", name, i+1, strings.TrimSpace(line))
			}
			if strings.Contains(line, "if *e {") {
				elementWrites++
			}
		}
	}
	if elementWrites == 0 {
		t.Fatal("no bool element write `if *e {` was emitted: the test reads nothing")
	}
}
