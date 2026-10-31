package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// A UNION ARM IS A FIELD LINE (docs/PORTING.md M18; docs/SPEC-TABLES.md §2.6,
// §3). An arm's payload is exactly the bytes a field puts after its own
// framing prefix, so the arm is read by the field's own reader with the arm's
// L standing in for the field's length: table_load.moo's read_arm checks
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
//   - Behavior against the engine: the conformance surfaces (wire, report,
//     json-write, retain), whose corpus carries an arm of every shape, and the
//     wire-fuzz differential (tables-moo-wire-fuzz), which frames them
//     hostilely.
//   - The reversed witness (TestMooArmFieldLineNegativeControl): a per-shape
//     reader planted into read_arm, and a table-arm reader planted into
//     read_union, each turn this scan.

// mooArmPayloadReaders are the calls a per-shape payload reader makes: if
// read_arm named one, the payload would be read there and not in read_field.
var mooArmPayloadReaders = []string{
	"V.utf8(", "V.wide(", "V.clampUtf8(", "V.clampWide(", "store_buffer(", "buffer.readstring(",
	"enum_read(", "array_body(", "read_array(", "read_keyed(", "read_map(", "read_body(", "scalar_at(",
}

// mooFunctionBody answers the source of the function opened by the line
// `opening`, up to the next `end` at column zero.
func mooFunctionBody(src, opening string) (string, bool) {
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

// mooArmFieldViolations answers what is wrong with a table_load.moo: read_arm
// must read through read_field and nothing per-shape, and read_union must
// leave every payload to read_arm.
func mooArmFieldViolations(src string) []string {
	var out []string
	arm, ok := mooFunctionBody(src, "local function read_arm(")
	if !ok {
		return []string{"read_arm is not where the gate looks for it"}
	}
	if !strings.Contains(arm, "read_field(") {
		out = append(out, "read_arm does not read through read_field")
	}
	for _, call := range mooArmPayloadReaders {
		if strings.Contains(arm, call) {
			out = append(out, "read_arm reads a payload itself with "+call)
		}
	}
	union, ok := mooFunctionBody(src, "read_union = function(")
	if !ok {
		return append(out, "read_union is not where the gate looks for it")
	}
	if !strings.Contains(union, "read_arm(") {
		out = append(out, "read_union does not read its arms through read_arm")
	}
	for _, call := range mooArmPayloadReaders {
		if call == "scalar_at(" {
			continue // the header's widening rule (see above)
		}
		if strings.Contains(union, call) {
			out = append(out, "read_union reads an arm's payload itself with "+call)
		}
	}
	return out
}

func mooTableLoadSource(t *testing.T) string {
	t.Helper()
	src := string(mootable.Runtime()["table_load.moo"])
	if src == "" {
		t.Fatal("the runtime carries no table_load.moo")
	}
	return src
}

func TestMooArmIsAFieldLine(t *testing.T) {
	t.Parallel()
	for _, v := range mooArmFieldViolations(mooTableLoadSource(t)) {
		t.Error(v)
	}
}

func TestMooArmFieldLineNegativeControl(t *testing.T) {
	t.Parallel()
	src := mooTableLoadSource(t)
	if got := mooArmFieldViolations(src); len(got) != 0 {
		t.Fatalf("the control needs a clean baseline: %v", got)
	}
	const anchor = "\tlocal s = arm.s\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the plant's anchor appears %d times, want 1: the control would be vacuous", strings.Count(src, anchor))
	}
	planted := strings.Replace(src, anchor, anchor+"\tif s == \"text\" and not V.utf8(sub.b, sub.at, sub.e - sub.at) then return false end\n", 1)
	got := mooArmFieldViolations(planted)
	if len(got) != 1 || !strings.Contains(got[0], "V.utf8(") {
		t.Fatalf("a per-shape text check planted into read_arm was not the one finding: %v", got)
	}
	const unionAnchor = "\telseif arm.s == \"void\" then"
	if strings.Count(src, unionAnchor) != 1 {
		t.Fatalf("the second plant's anchor appears %d times, want 1: the control would be vacuous", strings.Count(src, unionAnchor))
	}
	tab := strings.Replace(src, unionAnchor, "\telseif arm.s == \"tab\" then\n\t\tread_body(S.lookup(arm.t), sub, u, true)\n"+unionAnchor, 1)
	got = mooArmFieldViolations(tab)
	if len(got) != 1 || !strings.Contains(got[0], "read_body(") {
		t.Fatalf("a table-arm reader planted into read_union was not the one finding: %v", got)
	}
}
