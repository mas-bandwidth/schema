package compiler

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValueDefaultsEveryShape is the per-PR half of the value-defaults
// corpus rows (testdata/conformance/tables/MANIFEST.txt, THE VALUE-DEFAULTS
// ROWS; docs/SPEC.md §4.2): generation only, no interpreter, no compiler.
//
// Every registered target generates test/tables/ValueDefaults.schema, the
// unit that puts a string, a bytes and a flags default together on a field in
// every shape that carries one (plain, nested by value, counted array, fixed
// array, slice, enum-keyed array, map value, optional, pointer target, union
// arm), in a variable root and a fixed root. And a default written on the
// container itself is refused when the unit is checked, by its named rule, so
// no target ever meets one.
func TestValueDefaultsEveryShape(t *testing.T) {
	t.Parallel()
	u := unitFromPaths(t, []string{filepath.Join("..", "test", "tables", "ValueDefaults.schema")})
	// The fixture is held to its promised shapes and defaults before any
	// output is read, and each target's output to carrying both table roots
	// (valuedefaults_gate_test.go, with the controls each half must refuse).
	if err := valueDefaultsFixture(u); err != nil {
		t.Fatal(err)
	}
	c := New()
	targets := c.Targets()
	// 17 in the default build; moo joins under its build tag
	if len(targets) < 17 {
		t.Fatalf("only %d targets registered: %v", len(targets), targets)
	}
	for _, target := range targets {
		files, err := c.Generate(u, target, Options{})
		if err != nil {
			t.Errorf("%s refuses the value-defaults unit: %v", target, err)
			continue
		}
		if err := tableRootsEmitted(files, valueDefaultsRoots...); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
}

func TestValueDefaultsOnAContainerAreRefused(t *testing.T) {
	t.Parallel()
	const head = "package probe\n\nflags Caps { Jump, Crouch, Fly }\nenum Slot { Alpha, Beta }\n\n"
	cases := []struct {
		name, body, want string
	}{
		{"optional", "table Root\n{\n    s ?string(8) = \"x\"\n    link *Root\n}\n",
			"? on string(N) is a named follow-on"},
		{"text-list", "table Root\n{\n    s [..4]string(8) = \"x\"\n    link *Root\n}\n",
			"a text list takes no specified default"},
		{"enum-keyed-array", "table Root\n{\n    f [Slot]Caps = { Jump }\n    link *Root\n}\n",
			"an array takes no specified default"},
		{"map-value", "table Root\n{\n    m map[uint8]string(8) = \"x\"\n    link *Root\n}\n",
			"a map takes no specified default"},
		{"pointer-text", "table Root\n{\n    s *string = \"x\"\n    link *Root\n}\n",
			"takes no specified default — a fresh reference is null"},
		{"pointer-bytes", "table Root\n{\n    b *bytes = \"ab\"\n    link *Root\n}\n",
			"a byte buffer takes no specified default"},
		{"union-arm", "union U\n{\n    empty\n    a string(8) = \"x\"\n}\n\ntable Root\n{\n    u U\n    link *Root\n}\n",
			"arm a takes no specified default"},
	}
	for _, tc := range cases {
		_, err := unitFromText(head + tc.body)
		if err == nil {
			t.Errorf("%s: a default on the container was accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refused, but not by its rule:\n got: %v\nwant: %q", tc.name, err, tc.want)
		}
	}
}
