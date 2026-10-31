package luautable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// A UNION ARM IS A FIELD LINE (docs/PORTING.md M18; docs/SPEC-TABLES.md §2.6,
// §3). An arm's payload is exactly the bytes a field puts after its own
// framing prefix, so the arm is read by the field's own reader with the arm's
// L standing in for the field's length: schema_load.luau's read_arm checks
// framing and calls read_field, and nothing else reads an arm's payload. Three
// things hold it:
//
//   - Structure (this file): read_arm and read_union name no per-shape payload
//     reader. A second reader for arms beside the field's is the shape the
//     method refuses, and it is refused here by reading the source. read_union
//     keeps scalar_at for the one rule that is the HEADER's and not the
//     payload's: an arm whose kind byte WIDENS to the declared kind, which a
//     body applies at a field's header the same way (read_body), never in
//     read_field.
//   - Behaviour against the engine: TestLuauLoadUnionFramingAgreesWithTheEngine
//     and the wire-fuzz differential (tables-luau-wire-fuzz), whose corpus
//     carries every arm shape, framed hostilely by the enumerated pass.
//   - The reversed witness (TestLuauArmFieldLineNegativeControl): a per-shape
//     branch planted into read_arm turns this scan.

// luauArmPayloadReaders are the calls a per-shape payload reader makes: if
// read_arm named one, the payload would be read there and not in read_field.
var luauArmPayloadReaders = []string{
	"utf8_ok(", "wide_ok(", "clamp_utf8(", "clamp_wide(", "store_bytes(", "copy_out(",
	"enum_read(", "array_body(", "read_array(", "read_keyed(", "read_map(", "read_body(", "scalar_at(",
}

// luauFunctionBody answers the source of the function opened by the line
// `opening`, up to the next `end` at column zero.
func luauFunctionBody(src, opening string) (string, bool) {
	i := strings.Index(src, opening)
	if i < 0 {
		return "", false
	}
	rest := src[i:]
	j := strings.Index(rest, "\nend\n")
	if j < 0 {
		return "", false
	}
	return rest[:j+5], true
}

// luauArmFieldViolations answers what is wrong with a schema_load.luau:
// read_arm must read through read_field and nothing per-shape, and read_union
// must leave every payload to read_arm.
func luauArmFieldViolations(src string) []string {
	var out []string
	arm, ok := luauFunctionBody(src, "local function read_arm(")
	if !ok {
		return []string{"read_arm is not where the gate looks for it"}
	}
	if !strings.Contains(arm, "read_field(") {
		out = append(out, "read_arm does not read through read_field")
	}
	for _, call := range luauArmPayloadReaders {
		if strings.Contains(arm, call) {
			out = append(out, "read_arm reads a payload itself with "+call)
		}
	}
	union, ok := luauFunctionBody(src, "read_union = function(")
	if !ok {
		return append(out, "read_union is not where the gate looks for it")
	}
	if !strings.Contains(union, "read_arm(") {
		out = append(out, "read_union does not read its arms through read_arm")
	}
	for _, call := range luauArmPayloadReaders {
		if call == "scalar_at(" {
			continue // the header's widening rule (see above)
		}
		if strings.Contains(union, call) {
			out = append(out, "read_union reads an arm's payload itself with "+call)
		}
	}
	return out
}

func TestLuauArmIsAFieldLine(t *testing.T) {
	t.Parallel()
	src := string(luautable.Runtime()["schema_load.luau"])
	if src == "" {
		t.Fatal("the runtime carries no schema_load.luau")
	}
	for _, v := range luauArmFieldViolations(src) {
		t.Error(v)
	}
}

func TestLuauArmFieldLineNegativeControl(t *testing.T) {
	t.Parallel()
	src := string(luautable.Runtime()["schema_load.luau"])
	const anchor = "\tlocal s = arm.s\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the plant's anchor appears %d times, want 1: the control would be vacuous", strings.Count(src, anchor))
	}
	planted := strings.Replace(src, anchor, anchor+"\tif s == \"str\" and not utf8_ok(sub.b, sub.at, sub.e - sub.at) then return false end\n", 1)
	got := luauArmFieldViolations(planted)
	if len(got) != 1 || !strings.Contains(got[0], "utf8_ok(") {
		t.Fatalf("a per-shape text check planted into read_arm was not the one finding: %v", got)
	}
	tab := strings.Replace(src, "\telseif arm.s == \"void\" then", "\telseif arm.s == \"tab\" then\n\t\tread_body(S.lookup(arm.t), sub, u, true)\n\telseif arm.s == \"void\" then", 1)
	if tab == src {
		t.Fatal("the second plant's anchor is gone: the control would be vacuous")
	}
	got = luauArmFieldViolations(tab)
	if len(got) != 1 || !strings.Contains(got[0], "read_body(") {
		t.Fatalf("a table-arm reader planted into read_union was not the one finding: %v", got)
	}
}
