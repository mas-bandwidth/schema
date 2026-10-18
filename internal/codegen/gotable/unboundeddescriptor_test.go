package gotable

import (
	"testing"
)

// TestGoUnboundedDescriptorIsExplicit holds docs/SPEC-TABLES.md §8.1 in the
// Go backend, through a generated unit compiled and run in a t.TempDir():
//
//   - a map, an unbounded []T and an unbounded text list []string(8) report
//     UnboundedContainer true and ArrayBound 0 in their descriptor, so a
//     reflection reader can tell an unbounded field from a bound of 2^31-1;
//   - a bounded [..4]T reports UnboundedContainer false and ArrayBound 4;
//   - a bare `string` field reports the §2.13.1 Unbounded column (unbounded
//     text), not UnboundedContainer, so the two columns stay distinct in one
//     unit that mixes them;
//   - a round trip of 100000 map entries, 100000 []T elements and 100000
//     text-list elements through the binary wire, base64 (§16.8) and JSON
//     (§16.2) reads every entry.
func TestGoUnboundedDescriptorIsExplicit(t *testing.T) {
	t.Parallel()
	runGenerated(t, unboundedDescriptorSchema, unboundedDescriptorProbe)
}

const unboundedDescriptorSchema = `package probe

table Root
{
    tags map[int32]int32
    byname map[string]int32
    nums []int32
    words []string(8)
    quad [..4]int32
}
`

const unboundedDescriptorProbe = `package probe

import (
	"encoding/base64"
	"fmt"
	"testing"
)

type desc struct {
	list, m, isArray, unc, unb bool
	bound                      int32
}

func checkField(t *testing.T, name string, f *TableFieldInfo, want desc) {
	t.Helper()
	if f.Name != name || f.List != want.list || f.Map != want.m || f.IsArray != want.isArray ||
		f.UnboundedContainer != want.unc || f.Unbounded != want.unb || f.ArrayBound != want.bound {
		t.Fatalf("descriptor %s: got name=%q list=%v map=%v isArray=%v unc=%v unb=%v bound=%d; want %+v",
			name, f.Name, f.List, f.Map, f.IsArray, f.UnboundedContainer, f.Unbounded, f.ArrayBound, want)
	}
}

func TestDescriptor(t *testing.T) {
	fs := RootTableType().Fields
	if len(fs) != 5 {
		t.Fatalf("field count %d", len(fs))
	}
	checkField(t, "tags", &fs[0], desc{m: true, isArray: true, unc: true})
	checkField(t, "byname", &fs[1], desc{m: true, isArray: true, unc: true})
	checkField(t, "nums", &fs[2], desc{list: true, isArray: true, unc: true})
	checkField(t, "words", &fs[3], desc{list: true, isArray: true, unc: true})
	checkField(t, "quad", &fs[4], desc{isArray: true, bound: 4})
	if !fs[1].Table().Fields[0].Unbounded {
		t.Fatal("byname key is not unbounded text (§2.13.1)")
	}
}

func saveProbe(t *testing.T, r *Root) []byte {
	t.Helper()
	n := RootMeasure(r)
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	if RootSave(r, buf) != n {
		t.Fatal("Save")
	}
	return buf
}

func loadProbe(t *testing.T, wire []byte) *Root {
	t.Helper()
	size := RootLoadMeasure(wire)
	if size < 0 {
		t.Fatal("LoadMeasure refused")
	}
	region := make([]byte, size+64)
	if RootLoad(region, wire, nil) == nil {
		t.Fatal("load failed")
	}
	return RootLoad(region, wire, nil)
}

func TestRoundTrip(t *testing.T) {
	var b RootBuilder
	b.Init()
	defer b.Shutdown()
	v := b.GetRoot()
	const n = 100000
	for i := int32(0); i < n; i++ {
		*RootTagsInsert(&b.Main, &v.Tags, i) = i * 2
		*RootNumsAdd(&b.Main, &v.Nums) = i
		if !RootWordsAppend(&b.Main, v, fmt.Sprintf("w%d", i)) {
			t.Fatal("words append")
		}
	}
	if !b.Lock() {
		t.Fatal("lock")
	}
	r := b.AsConst()
	if r.Tags.Count != n || r.Nums.Count != n || r.Words.Count != n {
		t.Fatalf("counts tags=%d nums=%d words=%d", r.Tags.Count, r.Nums.Count, r.Words.Count)
	}
	wire := saveProbe(t, r)
	loaded := loadProbe(t, wire)
	if loaded == nil || loaded.Tags.Count != n || loaded.Nums.Count != n || loaded.Words.Count != n {
		t.Fatalf("binary round trip: tags=%d nums=%d words=%d", loaded.Tags.Count, loaded.Nums.Count, loaded.Words.Count)
	}
	b64 := make([]byte, base64.StdEncoding.EncodedLen(len(wire)))
	base64.StdEncoding.Encode(b64, wire)
	dec, err := base64.StdEncoding.DecodeString(string(b64))
	if err != nil {
		t.Fatal(err)
	}
	loaded2 := loadProbe(t, dec)
	if loaded2 == nil || loaded2.Tags.Count != n || loaded2.Nums.Count != n || loaded2.Words.Count != n {
		t.Fatalf("base64 round trip")
	}
	js := make([]byte, RootToJsonMeasure(r))
	if RootToJson(r, js) != int64(len(js)) {
		t.Fatal("ToJson")
	}
	var b2 RootBuilder
	b2.Init()
	defer b2.Shutdown()
	var report TableReport
	if !RootFromJson(&b2, js, &report) || report != (TableReport{}) {
		t.Fatalf("json read %+v", report)
	}
	if !b2.Lock() {
		t.Fatal("json lock")
	}
	r2 := b2.AsConst()
	if r2.Tags.Count != n || r2.Nums.Count != n || r2.Words.Count != n {
		t.Fatalf("json round trip: tags=%d nums=%d words=%d", r2.Tags.Count, r2.Nums.Count, r2.Words.Count)
	}
}
`
