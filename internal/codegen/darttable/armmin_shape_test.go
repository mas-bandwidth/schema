package darttable

import (
	"maps"
	"strings"
	"testing"
)

// armMinShapeSchema holds a counted [2..4] array as a union arm and as a plain
// field, on a variable root and a fixed one.
const armMinShapeSchema = `package probe

union Pick
{
    few   [2..4]uint32
    other int32
}

table V
{
    pick Pick
    xs   [2..4]uint32
}

fixed table F
{
    pick Pick
    xs   [2..4]uint32
}
`

// TestDartArmMinShape pins, in the generated source and with no toolchain,
// ruling R10: a counted [A..B] holds A wherever the field sits
// (docs/SPEC-TABLES.md §3.4). A count below A refuses count_over_bound on the
// variable wire read (an arm, an inert arm payload, a field), on the JSON read
// (arm and field), and on the fixed-form decode (arm and field); the cook
// writer asserts A (Dart write checks are debug asserts). Before, every one of
// these held only B, so an arm holding one element of [2..4] read clean.
// The fixed-form decode and the JSON read are also held by the shared rows
// armmin_fix_count_{0,1} and arm-min-fix-{below,empty}.
func TestDartArmMinShape(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, armMinShapeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	all := map[string]string{}
	for name, src := range files {
		all[name] = string(src)
	}
	has := func(want string) bool {
		for _, src := range all {
			if strings.Contains(src, want) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		// variable wire: the arm, its inert payload, and the plain field
		"if (count < 2) { p.report.refuse('count_over_bound'); return false; }",
		"if (p.limit - p.pos < 2) { p.report.refuse('count_over_bound'); return false; }",
		"if (keep < 2) { r.report.refuse('count_over_bound'); return; }",
		// JSON read: the arm (through jsonReadExpr) and the plain field
		"v.few = _jsonAtLeast(_jsonList<int>(",
		"if (v.xs.length < 2) { r.refuse('count_over_bound'); }",
		// fixed-form decode: both plans run it
		"if (value.pick.fewCount < 2) {",
		"if (value.xsCount < 2) {",
		// cook writer: a debug assert on the minimum
		"assert(value.pick.few.length >= 2);",
		"assert(value.xs.length >= 2);",
	} {
		if !has(want) {
			t.Errorf("no generated file holds %s", want)
		}
	}
}
