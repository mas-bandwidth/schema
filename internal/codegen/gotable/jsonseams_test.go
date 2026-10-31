package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoJsonWalkSeamsFailClosed is the seam-break probe for the JSON walker's
// unbounded-text and optional-map seams: each anchor, perturbed or doubled, must
// panic at generation naming it, and the unperturbed walk must carry every patch.
func TestGoJsonWalkSeamsFailClosed(t *testing.T) {
	t.Parallel()

	const schema = `package probe
table Leaf { v int32 }
table Root {
  names []string
  m ?map[int32]Leaf
}
`
	u, base := buildJsonWalkBase(t, schema)

	type seam struct {
		name  string
		old   string
		apply func(*ir.Unit, string) string
	}
	seams := []seam{
		{"unbounded-writeScalar", "func tableJSONWriteScalar(out *tableJSONOut, storage unsafe.Pointer, f *TableFieldInfo, depth int32) bool {\n", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"unbounded-read-kind33", " if f.Kind==33 {units:=unsafe.Slice((*uint16)(storage),f.ArrayBound)", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"unbounded-write-textbytes", "n:=tableJSONCount(p,v);if n<0||n>v.ArrayBound{return false};tableJSONWriteString(out,tableJSONBytes(unsafe.Add(p,uintptr(v.Offset)),n));return true}", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"unbounded-read-key", "if key.Kind==12 {if n>key.ArrayBound {in.report.Clamped++;in.pos=probe.pos;good=false}else{", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"unbounded-map-store", "entry.Reset(slot);tableMapStoreKey(slot,key,k);", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"unbounded-source", "// ---- json walk: end ----", func(u *ir.Unit, s string) string { return unboundedTextJson(u, s, true) }},
		{"optional-map-reset", "\t\t\t\tif f.Table != nil {\n\t\t\t\t\tf.Table().Reset(unsafe.Add(base, uintptr(f.Offset)))", optionalMapJson},
	}

	for _, sm := range seams {
		t.Run(sm.name, func(t *testing.T) {
			t.Parallel()
			perturbed := strings.Replace(base, sm.old, perturbOneByte(sm.old), 1)
			if perturbed == base {
				t.Fatal("perturb did not change the anchor")
			}
			assertPanicContains(t, func() { sm.apply(u, perturbed) }, "occurs 0 times, want 1")
			doubled := strings.Replace(base, sm.old, sm.old+sm.old, 1)
			if doubled == base {
				t.Fatal("doubling did not change the anchor")
			}
			assertPanicContains(t, func() { sm.apply(u, doubled) }, "occurs 2 times, want 1")
		})
	}

	t.Run("whole-walk", func(t *testing.T) {
		t.Parallel()
		s := unboundedTextJson(u, base, true)
		s = optionalMapJson(u, s)
		for _, marker := range []string{"tableJSONReadText(", "tableTextBytes(", "tableMapStoreKeyed(", "if f.Map {"} {
			if !strings.Contains(s, marker) {
				t.Errorf("patch marker %q missing from the generated walk", marker)
			}
		}
	})
}

// buildJsonWalkBase assembles the shared JSON walker the way generateJsonFiles
// does (json.go), for a unit with unbounded text and an optional map.
func buildJsonWalkBase(t *testing.T, src string) (*ir.Unit, string) {
	t.Helper()
	u := unitFrom(t, src)
	walk := tableJSONWalkSource
	if unitHasRequired(u) {
		walk = tableJSONRequiredWalk(walk, u)
	}
	extra := tableJSONWideSource + tableJSONGraphSource + tableJSONListSource + tableJSONMapSource + tableJSONMapDecimalSource + timestampParserSource() + tableJSONTimestampSource + tableTimestampRuntime
	walk = tableSourceReplace(walk, "// ---- json walk: end ----", extra+"// ---- json walk: end ----", 1)
	walk = textListJson(u, walk, true)
	return u, walk
}

// perturbOneByte flips the lowest bit of the anchor's first byte so the copy
// differs from the marker at exactly one byte.
func perturbOneByte(s string) string {
	b := []byte(s)
	b[0] ^= 0x01
	return string(b)
}

// assertPanicContains runs f and requires it to panic with a message containing
// want (the fail-closed count the seam helper reports).
func assertPanicContains(t *testing.T, f func(), want string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic naming the seam, got none (want %q)", want)
		}
		if !strings.Contains(fmt.Sprint(r), want) {
			t.Fatalf("panic %v does not contain %q", r, want)
		}
	}()
	f()
}
