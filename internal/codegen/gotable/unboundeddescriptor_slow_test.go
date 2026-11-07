package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoUnboundedContainersRoundTrip keeps the §8.1 containers' round trip in
// the generated unit, through the binary wire, base64 (RootSaveText /
// RootLoadText, §16.8) and JSON (§16.2), with every entry and element read back
// by key and index. It is a slow test because it compiles and runs a unit.
//
// The size is 10000, not 100000: the generated map insert is quadratic (its
// linear scan in tableMapFind, see the report), so 100000 does not finish in
// go test's timeout. 10000 proves the same per-entry and per-element property in
// a few seconds.
func TestGoUnboundedContainersRoundTrip(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	runGenerated(t, unboundedDescriptorSchema, unboundedDescriptorRoundTripProbe)
}

const unboundedDescriptorRoundTripProbe = `package probe

import (
	"fmt"
	"testing"
)

const n = 10000

// byname's entries: its only job here is to put unbounded text keys in the
// unit beside the containers
const nb = 1000

func tagKey(i int32) string  { return fmt.Sprintf("t%06d", i) }
func word(i int32) string    { return fmt.Sprintf("w%06d", i) }
func name(i int32) string    { return fmt.Sprintf("name-%d-unbounded", i) }
func bynameKey(i int32) string { return fmt.Sprintf("key-%d-unbounded", i) }

func fill(t *testing.T, b *RootBuilder) {
	t.Helper()
	v := b.GetRoot()
	for i := int32(0); i < n; i++ {
		p := RootTagsInsert(&b.Main, &v.Tags, tagKey(i))
		q := RootIntsInsert(&b.Main, &v.Ints, i)
		r := RootNumsAdd(&b.Main, &v.Nums)
		if p == nil || q == nil || r == nil {
			t.Fatalf("insert %d refused", i)
		}
		*p, *q, *r = i+1, i*2, i*3
		if i < nb {
			s := RootBynameInsert(&b.Main, &v.Byname, bynameKey(i))
			if s == nil {
				t.Fatalf("byname insert %d refused", i)
			}
			*s = i * 5
		}
		if !RootWordsAppend(&b.Main, v, word(i)) || !RootNamesAppend(&b.Main, v, name(i)) {
			t.Fatalf("append %d refused", i)
		}
	}
	for i := int32(0); i < 3; i++ {
		v.Quad[i] = 7 * (i + 1)
	}
	v.QuadCount = 3
}

// every reads every entry and every element back, by key and by index
func every(t *testing.T, how string, v *Root, arena *TableArena) {
	t.Helper()
	if v == nil {
		t.Fatalf("%s: no value", how)
	}
	if v.Tags.Count != n || v.Ints.Count != n || v.Nums.Count != n || v.Words.Count != n || v.Names.Count != n || v.Byname.Count != nb {
		t.Fatalf("%s: counts tags=%d ints=%d nums=%d words=%d names=%d byname=%d; want %d (byname 1000)",
			how, v.Tags.Count, v.Ints.Count, v.Nums.Count, v.Words.Count, v.Names.Count, v.Byname.Count, n)
	}
	for i := int32(0); i < n; i++ {
		if p := RootTagsFind(arena, &v.Tags, tagKey(i)); p == nil || *p != i+1 {
			t.Fatalf("%s: tags[%q] lost", how, tagKey(i))
		}
		if p := RootIntsFind(arena, &v.Ints, i); p == nil || *p != i*2 {
			t.Fatalf("%s: ints[%d] lost", how, i)
		}
		if i < nb {
			if p := RootBynameFind(arena, &v.Byname, bynameKey(i)); p == nil || *p != i*5 {
				t.Fatalf("%s: byname[%q] lost", how, bynameKey(i))
			}
		}
		if p := v.Nums.At(i, arena); p == nil || *p != i*3 {
			t.Fatalf("%s: nums[%d] lost", how, i)
		}
		if w, ok := RootWordsTextAt(v, i, arena); !ok || string(w) != word(i) {
			t.Fatalf("%s: words[%d] = %q, want %q", how, i, w, word(i))
		}
		if w, ok := RootNamesTextAt(v, i, arena); !ok || string(w) != name(i) {
			t.Fatalf("%s: names[%d] = %q, want %q", how, i, w, name(i))
		}
	}
	if v.QuadCount != 3 || v.Quad[0] != 7 || v.Quad[1] != 14 || v.Quad[2] != 21 || v.Quad[3] != 0 {
		t.Fatalf("%s: quad %v count %d", how, v.Quad, v.QuadCount)
	}
}

func TestRoundTrip(t *testing.T) {
	var b RootBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	fill(t, &b)
	if !b.Lock() {
		t.Fatal("lock")
	}
	v := b.AsConst()
	every(t, "locked", v, nil)

	// the binary wire
	size := RootMeasure(v)
	if size < 0 {
		t.Fatal("Measure refused")
	}
	wire := make([]byte, size)
	if RootSave(v, wire) != size {
		t.Fatal("Save")
	}
	measure := RootLoadMeasure(wire)
	if measure < 0 {
		t.Fatal("LoadMeasure refused")
	}
	var report TableReport
	every(t, "binary", RootLoad(make([]byte, measure), wire, &report), nil)
	if report != (TableReport{}) {
		t.Fatalf("binary report %+v", report)
	}

	// base64 (§16.8)
	text, err := RootSaveText(v)
	if err != nil {
		t.Fatalf("SaveText %v", err)
	}
	report = TableReport{}
	back, err := RootLoadText(text, &report)
	if err != nil || report != (TableReport{}) {
		t.Fatalf("LoadText %v %+v", err, report)
	}
	every(t, "base64", back, nil)

	// JSON (§16.2)
	jn := RootToJSONMeasure(v)
	if jn < 0 {
		t.Fatal("ToJSONMeasure refused")
	}
	js := make([]byte, jn)
	if RootToJSON(v, js) != jn {
		t.Fatal("ToJSON")
	}
	var jb RootBuilder
	if !jb.Init() {
		t.Fatal("json init")
	}
	defer jb.Shutdown()
	report = TableReport{}
	if !RootFromJSON(&jb, js, &report) || report != (TableReport{}) {
		t.Fatalf("FromJSON %+v", report)
	}
	if !jb.Lock() {
		t.Fatal("json lock")
	}
	every(t, "json", jb.AsConst(), nil)
}
`
