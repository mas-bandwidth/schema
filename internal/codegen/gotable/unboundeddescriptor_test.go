package gotable

import (
	"testing"
)

// TestGoUnboundedDescriptorIsExplicit holds docs/SPEC-TABLES.md §8.1 (the
// UnboundedContainer column, with array_bound 0 beside it) in the Go backend,
// through a generated unit compiled and run by runGenerated in a t.TempDir():
//
//   - a map (map[string(8)]int32 and map[int32]int32), a []T ([]int32) and a
//     text list []string(8) report UnboundedContainer true and ArrayBound 0, so
//     a reflection reader can tell "no bound" from a bound of 2^31 - 1;
//   - a bounded [..4]T reports UnboundedContainer false and ArrayBound 4;
//   - the same unit mixes unbounded TEXT (a bare-string text list []string and
//     a map[string]int32 key, §2.13.1) with the containers: the generated
//     TableFieldInfo carries both columns once each (it compiles), the text
//     element and the text key report Unbounded true and UnboundedContainer
//     false, and the container fields report Unbounded false;
//   - 100000 entries in each of the two maps tags and ints and 100000
//     elements in each of the lists nums, words and names round trip through
//     the binary wire, base64 (RootSaveText / RootLoadText, §16.8) and JSON
//     (§16.2), and every entry and element is read back by key and by index;
//     byname, the map that carries unbounded text keys, rides with 1000.
func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	runGenerated(t, unboundedDescriptorSchema, unboundedDescriptorProbe)
}

const unboundedDescriptorSchema = `package probe

table Root
{
    tags map[string(8)]int32
    ints map[int32]int32
    nums []int32
    words []string(8)
    quad [..4]int32
    names []string
    byname map[string]int32
}
`

const unboundedDescriptorProbe = `package probe

import (
	"fmt"
	"testing"
)

const n = 100000

// byname's entries: its only job here is to put unbounded text keys in the
// unit beside the containers
const nb = 1000

type want struct {
	isArray, list, isMap, container, text bool
	bound                                  int32
}

func checkField(t *testing.T, f *TableFieldInfo, name string, w want) {
	t.Helper()
	if f.Name != name || f.IsArray != w.isArray || f.List != w.list || f.Map != w.isMap ||
		f.UnboundedContainer != w.container || f.Unbounded != w.text || f.ArrayBound != w.bound {
		t.Fatalf("descriptor %s: name=%q IsArray=%v List=%v Map=%v UnboundedContainer=%v Unbounded=%v ArrayBound=%d; want %s %+v",
			name, f.Name, f.IsArray, f.List, f.Map, f.UnboundedContainer, f.Unbounded, f.ArrayBound, name, w)
	}
}

func TestDescriptor(t *testing.T) {
	fs := RootTableType().Fields
	if len(fs) != 7 {
		t.Fatalf("field count %d, want 7", len(fs))
	}
	container := func(list bool) want { return want{isArray: true, list: list, isMap: !list, container: true} }
	checkField(t, &fs[0], "tags", container(false))
	checkField(t, &fs[1], "ints", container(false))
	checkField(t, &fs[2], "nums", container(true))
	checkField(t, &fs[3], "words", container(true))
	checkField(t, &fs[4], "quad", want{isArray: true, bound: 4})
	checkField(t, &fs[5], "names", container(true))
	checkField(t, &fs[6], "byname", container(false))

	// unbounded TEXT is the other column (§2.13.1): on the element and the key
	if e := &fs[5].Table().Fields[0]; !e.Unbounded || e.UnboundedContainer {
		t.Fatalf("names element %q: Unbounded=%v UnboundedContainer=%v; want true false", e.Name, e.Unbounded, e.UnboundedContainer)
	}
	if k := &fs[6].Table().Fields[0]; k.Name != "key" || !k.Unbounded || k.UnboundedContainer {
		t.Fatalf("byname key %q: Unbounded=%v UnboundedContainer=%v; want true false", k.Name, k.Unbounded, k.UnboundedContainer)
	}
	// bounded text is neither
	for _, e := range []*TableFieldInfo{&fs[0].Table().Fields[0], &fs[3].Table().Fields[0]} {
		if e.Unbounded || e.UnboundedContainer || e.ArrayBound != 8 {
			t.Fatalf("bounded text %q: Unbounded=%v UnboundedContainer=%v ArrayBound=%d; want false false 8", e.Name, e.Unbounded, e.UnboundedContainer, e.ArrayBound)
		}
	}
}

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
	jn := RootToJsonMeasure(v)
	if jn < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	js := make([]byte, jn)
	if RootToJson(v, js) != jn {
		t.Fatal("ToJson")
	}
	var jb RootBuilder
	if !jb.Init() {
		t.Fatal("json init")
	}
	defer jb.Shutdown()
	report = TableReport{}
	if !RootFromJson(&jb, js, &report) || report != (TableReport{}) {
		t.Fatalf("FromJson %+v", report)
	}
	if !jb.Lock() {
		t.Fatal("json lock")
	}
	every(t, "json", jb.AsConst(), nil)
}
`
