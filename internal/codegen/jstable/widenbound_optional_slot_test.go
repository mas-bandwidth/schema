package jstable

import (
	"regexp"
	"testing"
)

// TestOptionalFixedArraySlotIsTheElementDefault holds the element descriptor
// of an OPTIONAL fixed array `?[N]T` to its plain twin `[N]T`: a slot's
// default is the element's, never ABSENT. The variable read pads a fixed
// array stored short of the reader's bound with that default, one per new
// slot (docs/SPEC-TABLES.md §21.1, "a grown bound"), so a null there left the
// grown slot of `?[2]uint32` null, and ToJson refused the value the read had
// accepted (the widenbound_envelope conformance row, checkpoints [11, null]).
//
// No node: the descriptor is the generated source, and the def it carries is
// the value TableVariableElemDefault hands the pad.
func TestOptionalFixedArraySlotIsTheElementDefault(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

enum Mode { Read, Write }

table Root
{
    pu [2]uint32
    ou ?[2]uint32
    pw [2]int64
    ow ?[2]int64
    pf [2]float32
    of ?[2]float32
    pb [2]bool
    ob ?[2]bool
    pm [2]Mode
    om ?[2]Mode
}
`)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	src, ok := files[runtimeHome(u)+"Variable.js"]
	if !ok {
		t.Fatalf("no variable module %sVariable.js generated", runtimeHome(u))
	}
	// one codec field descriptor: { n: "<name>", ... e: { ... def: <x> } }
	elemDef := func(name string) string {
		t.Helper()
		re := regexp.MustCompile(`\{ n: "` + name + `", key: "` + name + `",[^\n]* e: \{[^\n]*def: ([^,}]+(?:\(\))?) \} \},?\n`)
		m := re.FindSubmatch(src)
		if m == nil {
			t.Fatalf("no codec descriptor with an element def for field %s", name)
		}
		return string(m[1])
	}
	for _, pair := range [][2]string{{"pu", "ou"}, {"pw", "ow"}, {"pf", "of"}, {"pb", "ob"}, {"pm", "om"}} {
		plain, optional := elemDef(pair[0]), elemDef(pair[1])
		if optional != plain {
			t.Errorf("?[2] %s pads a grown slot with %s, [2] %s with %s: a slot of an optional array is the element's default, never absent (SPEC-TABLES §21.1)",
				pair[1], optional, pair[0], plain)
		}
	}
	// the scalar slots are concrete zeros, so a slot is never null
	for _, name := range []string{"ou", "ow", "of", "ob"} {
		if d := elemDef(name); d == "null" {
			t.Errorf("?[2] %s pads a grown slot with null", name)
		}
	}
}
