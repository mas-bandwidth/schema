package ir_test

// The BUILD VERSION (SPEC-TABLES.md §20), pinned against the page's own worked
// example — projection TEXT and digest both, so this port reproduces the text
// and not only the number.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Demo.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Demo.schema", Name: "Demo.schema", Base: "Demo", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

const workedSource = `package demo

enum Grade { Bronze, Silver, Gold }

table ShipConfig
{
    damage float32 = 21.0
    speed  float32 = 500.0 | was = "velocity"
    armor  uint8 | min = 0, max = 100
    grade  Grade = Silver
}
`

// §20.2's worked example, to the character. Every number in it derives from a
// rule on a page and none of it is declared, so a second implementation
// reproduces it — and a reader did, independently, before this one existed.
const workedProjection = `schema-build-version 1
protocol 0123456789abcdef
byteorder little
record ShipConfig sizeof=12 alignof=4
    field 15a9 kind=10 offset=0 size=4 default=21.0
    field 2e46 kind=10 offset=4 size=4 default=500.0
    field 7c9d kind=6 offset=8 size=1 min=0 max=100
    field d272 kind=7 offset=9 size=1 enum=Grade default=Silver
enum Grade
    variant 1 Bronze
    variant 2 Silver
    variant 3 Gold
`

func TestCookProjectionMatchesThePagesWorkedExample(t *testing.T) {
	u := unitFrom(t, workedSource)
	// the page works the example under a stated protocol id rather than the
	// one this source happens to produce; the id is an INPUT to the projection
	u.ProtocolId = 0x0123456789abcdef

	got := ir.CookProjection(u)
	if got != workedProjection {
		t.Errorf("the cook projection is not the page's (SPEC-TABLES.md §20.2).\n--- got ---\n%s\n--- want ---\n%s", got, workedProjection)
	}
	if v := ir.BuildVersion(u); v != 0x7402a36de22d9728 {
		t.Errorf("build version = 0x%016x, want 0x7402a36de22d9728 (SPEC-TABLES.md §20.2)", v)
	}
}

// The same unit with NO TABLE at all — its three header lines, that same
// protocol id, and nothing else. It is deliberately not equal to the protocol
// id, so no caller can substitute one for the other by accident.
func TestCookProjectionOfATablelessUnit(t *testing.T) {
	u := unitFrom(t, "package demo\n\ntype Point\n{\n    x float32\n}\n")
	u.ProtocolId = 0x0123456789abcdef
	want := "schema-build-version 1\nprotocol 0123456789abcdef\nbyteorder little\n"
	if got := ir.CookProjection(u); got != want {
		t.Errorf("a table-free unit projects its header lines alone.\n--- got ---\n%s", got)
	}
	if v := ir.BuildVersion(u); v != 0x49947af3382f914e {
		t.Errorf("build version = 0x%016x, want 0x49947af3382f914e (SPEC-TABLES.md §20.2)", v)
	}
	if v := ir.BuildVersion(u); v == u.ProtocolId {
		t.Error("the two ids are equal — one could be substituted for the other by accident")
	}
}

// §20.8's INDEPENDENCE PAIR, both directions. The second is the one that must
// never regress.
func TestBuildVersionIndependenceFromTheProtocolId(t *testing.T) {
	base := unitFrom(t, workedSource)
	baseVersion, baseProtocol := ir.BuildVersion(base), base.ProtocolId

	// a TABLE edit moves the build version and NOT the protocol id
	tableEdit := unitFrom(t, strings.Replace(workedSource, "damage float32 = 21.0", "damage float32 = 22.0", 1))
	if ir.BuildVersion(tableEdit) == baseVersion {
		t.Error("a table edit did not move the build version")
	}
	if tableEdit.ProtocolId != baseProtocol {
		t.Error("a table edit moved the PROTOCOL id — tables are never in the type wire (§10)")
	}

	// a TYPE edit moves BOTH
	typeEdit := unitFrom(t, workedSource+"\ntype Point\n{\n    x float32\n}\n")
	if typeEdit.ProtocolId == baseProtocol {
		t.Error("a type edit did not move the protocol id")
	}
	if ir.BuildVersion(typeEdit) == baseVersion {
		t.Error("a type edit did not move the build version — the protocol id rides in whole (§20.1 group 1)")
	}
}

// §20.8's batteries. Each row names an edit and whether it must move the id;
// the REFERENT rows are the ones a digest without type=/enum=/union=/key=/
// payload= passes in silence, and the exclusion rows are the ones a digest
// that swept everything in would fail.
func TestBuildVersionMovesOnWhatItMustAndNotOnWhatItMustNot(t *testing.T) {
	const src = `package demo

enum Grade { Bronze, Silver, Gold }
enum Rank { Cadet, Pilot, Ace }

flags Perks { Shielded, Cloaked }
flags Boons { Warded, Veiled }

type Buff
{
    multiplier float32
}

type Debuff
{
    amount int32
}

union Effect
{
    up   Buff
    down Debuff
}

table Row
{
    slot   int32 | min = 0, max = 10
    width  bits(6)
    ratio  float32 | min = 0.0, max = 1.0, resolution = 0.01
    grade  Grade = Silver
    perks  Perks
    buff   Buff
    scores [Grade]int32
    fx     Effect
    label  string(8) | json = "name"
    guard  bool
    if guard
    {
        extra int32
    }
}
`
	base := ir.BuildVersion(unitFrom(t, src))

	moves := []struct {
		what string
		from string
		to   string
	}{
		// the MEANING group, each isolating a fact no layout line sees
		{"a specified default changed", "grade  Grade = Silver", "grade  Grade = Gold"},
		{"a declared range tightened", "slot   int32 | min = 0, max = 10", "slot   int32 | min = 0, max = 9"},
		{"a bits(N) narrowed within one storage width", "width  bits(6)", "width  bits(5)"},
		{"a compressed float's resolution changed", "resolution = 0.01", "resolution = 0.02"},
		{"an enum variant renamed", "enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Silver, Aurum }"},
		{"two enum variants swapped", "enum Grade { Bronze, Silver, Gold }", "enum Grade { Silver, Bronze, Gold }"},
		{"a union arm renamed", "    up   Buff", "    rise Buff"},
		// the LAYOUT group's own controls
		{"a field's kind changed with its width unmoved", "slot   int32 | min = 0, max = 10", "slot   float32 | min = 0.0, max = 10.0, resolution = 0.5"},
		{"a field's offset moved with the record's sizeof unmoved", "    grade  Grade = Silver\n    perks  Perks", "    perks  Perks\n    grade  Grade = Silver"},
		{"a string capacity changed", "string(8)", "string(16)"},
		// the REFERENT controls: same-shape swaps every other fact survives
		{"a field retyped between two records of identical sizeof and alignof", "buff   Buff", "buff   Debuff"},
		{"a keyed array's KEY enum swapped for another of the same variant count", "scores [Grade]int32", "scores [Rank]int32"},
		{"a union arm's payload swapped for a same-shaped record", "    up   Buff", "    up   Debuff"},
	}
	for _, m := range moves {
		edited := strings.Replace(src, m.from, m.to, 1)
		if edited == src {
			t.Fatalf("%s: the edit patched nothing — the fixture drifted", m.what)
		}
		if got := ir.BuildVersion(unitFrom(t, edited)); got == base {
			t.Errorf("%s: the build version did not move", m.what)
		}
	}

	stays := []struct {
		what string
		from string
		to   string
	}{
		{"a was rename", "slot   int32 | min = 0, max = 10", "position int32 | was = \"slot\", min = 0, max = 10"},
		{"a flags variant reordered", "flags Perks { Shielded, Cloaked }", "flags Perks { Cloaked, Shielded }"},
		{"a flags variant renamed", "flags Perks { Shielded, Cloaked }", "flags Perks { Warded, Cloaked }"},
		{"a flags field's referent swapped for a same-width other", "perks  Perks", "perks  Boons"},
		{"a guard removed", "    if guard\n    {\n        extra int32\n    }\n", "    extra int32\n"},
		{"a json key changed", `json = "name"`, `json = "label"`},
		{"a comment added", "table Row", "// a comment\ntable Row"},
	}
	for _, s := range stays {
		edited := strings.Replace(src, s.from, s.to, 1)
		if edited == src {
			t.Fatalf("%s: the edit patched nothing — the fixture drifted", s.what)
		}
		if got := ir.BuildVersion(unitFrom(t, edited)); got != base {
			t.Errorf("%s: the build version moved (0x%016x -> 0x%016x), and no cook byte depends on it", s.what, base, got)
		}
	}
}

// §20.8's inclusions the sort order could hide.
func TestBuildVersionSeesARecordRenamed(t *testing.T) {
	base := ir.BuildVersion(unitFrom(t, workedSource))
	renamed := ir.BuildVersion(unitFrom(t, strings.ReplaceAll(workedSource, "ShipConfig", "ShipSetup")))
	if renamed == base {
		t.Error("a record renamed did not move the build version — the record line carries the name")
	}
	added := ir.BuildVersion(unitFrom(t, workedSource+"\ntable Extra\n{\n    n int32\n}\n"))
	if added == base {
		t.Error("a record added did not move the build version")
	}
}
