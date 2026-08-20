package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

// build compiles a one-file unit and returns it, failing the test on any error.
func build(t *testing.T, source string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(path, []byte(source))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

const baseSchema = `package probe

const MaxHealth = 1000

enum Team { Red, Blue }

type Vec3 {
    x float64
    y float64
    z float64
}

message State {
    team     Team
    position Vec3
    health   int32 [min = 0, max = MaxHealth]
    at_rest  bool
    if !at_rest {
        velocity Vec3
    }
}
`

// The point of the projection: an edit that moves no bytes must not move the
// protocol id. Each of these used to force a coordinated redeploy for nothing.
func TestIdIsStableUnderNonWireEdits(t *testing.T) {
	base := build(t, baseSchema).ProtocolId

	cases := []struct {
		name   string
		source string
	}{
		{"a trailing comment", baseSchema + "\n// a comment costs no bits\n"},
		{"an interior comment", strings.Replace(baseSchema, "message State {", "// describes a ship\nmessage State {", 1)},
		{"blank lines", strings.Replace(baseSchema, "enum Team", "\n\nenum Team", 1)},
		{"an enum variant renamed", strings.Replace(baseSchema, "{ Red, Blue }", "{ Crimson, Blue }", 1)},
		{"a const renamed", strings.NewReplacer("MaxHealth", "HealthCeiling").Replace(baseSchema)},
	}

	for _, tc := range cases {
		got := build(t, tc.source).ProtocolId
		if got != base {
			t.Errorf("%s moved the protocol id (0x%016x -> 0x%016x); no wire byte changed",
				tc.name, base, got)
		}
	}
}

// The other direction, and the one that must never regress: an edit that DOES
// move bytes must move the id. A missed case here is two incompatible builds
// claiming compatibility.
func TestIdMovesUnderWireEdits(t *testing.T) {
	base := build(t, baseSchema).ProtocolId

	cases := []struct {
		name   string
		source string
	}{
		{"a widened bound", strings.Replace(baseSchema, "const MaxHealth = 1000", "const MaxHealth = 2000", 1)},
		{"a field renamed (table identity is the name hash)",
			strings.Replace(baseSchema, "health   int32", "hp       int32", 1)},
		{"a field reordered", strings.Replace(baseSchema,
			"    team     Team\n    position Vec3", "    position Vec3\n    team     Team", 1)},
		{"a new enum variant (the tag range widens)",
			strings.Replace(baseSchema, "{ Red, Blue }", "{ Red, Blue, Green }", 1)},
		{"a field added", strings.Replace(baseSchema, "    at_rest  bool", "    shield   int32 [min = 0, max = 100]\n    at_rest  bool", 1)},
		{"a branch inverted", strings.Replace(baseSchema, "if !at_rest {", "if at_rest {", 1)},
		{"a type widened", strings.Replace(baseSchema, "    x float64", "    x float32", 1)},
		{"the package renamed", strings.Replace(baseSchema, "package probe", "package other", 1)},
	}

	for _, tc := range cases {
		got := build(t, tc.source).ProtocolId
		if got == base {
			t.Errorf("%s did NOT move the protocol id (0x%016x) — two incompatible builds would claim compatibility",
				tc.name, base)
		}
	}
}

// The projection is a reviewable artifact, so it has to be deterministic:
// same unit, same text, every time and in any map iteration order.
func TestProjectionIsDeterministic(t *testing.T) {
	first := ir.WireProjection(build(t, baseSchema))
	for i := range 20 {
		if got := ir.WireProjection(build(t, baseSchema)); got != first {
			t.Fatalf("projection is not deterministic on run %d", i)
		}
	}
	if !strings.HasPrefix(first, "schema-wire-projection ") {
		t.Error("the projection must carry its version on the first line")
	}
}

// A default is part of the TABLE wire, because a field sitting at its default
// is elided. So changing one must move the id.
func TestDefaultsAreWireFacts(t *testing.T) {
	withDefault := `package probe

table Config {
    retries int32 = -1
}
`
	other := strings.Replace(withDefault, "= -1", "= -2", 1)
	if build(t, withDefault).ProtocolId == build(t, other).ProtocolId {
		t.Error("changing a specified default did not move the id — the table wire elides a field at its default, so it IS a wire fact")
	}
}
