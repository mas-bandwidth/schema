package gotable

import (
	"testing"
)

// TestGoTextListTotalBytesBound holds docs/SPEC-TABLES.md §2.13's TOTAL,
// `| total = N` on a text list, in the Go backend, through a generated unit
// compiled and run in a t.TempDir(). Every Go writer and reader path the
// standard names is driven, on all four spellings ([..N]string(M),
// [..N]?string(M), []string(M), []?string(M)):
//
//   - within the total the value round-trips the binary wire, base64 (§16.8)
//     and the JSON text, each read reporting nothing;
//   - THE WRITERS: Append refuses the element that would carry the sum past
//     the total and the stored value does not move (AppendAbsent, which adds
//     nothing to the sum, is still taken); Save and Measure return -1 for a
//     value past it, a bounded list and an unbounded one, a nested table's
//     included; ToJsonMeasure returns -1; the retaining save returns -1;
//   - THE READERS: a wire written by a table with the same field ids and no
//     total (Wide), past the total on each list in turn, reads with the
//     longest prefix within the total kept, the rest dropped, clamped counted
//     once and named in Reason, the kept elements' sum within N; ReadStrict
//     refuses it with TableRefuseClamped naming the field; base64, the
//     retaining load and a nested table read it the same way; a JSON text
//     past the total reads with the same prefix kept, clamped counted once and
//     named, and report.Err() refuses it naming the field.
func TestGoTextListTotalBytesBound(t *testing.T) {
	t.Parallel()
	runGenerated(t, listTotalSchema, listTotalProbe)
}

const listTotalSchema = `package probe

table L
{
    names [..8]string(8) | total = 12
    maybe [..8]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table Wide
{
    names [..8]string(8)
    maybe [..8]?string(8)
    repos []string(8)
    bases []?string(8)
}

table Holder
{
    inner L
}

table HolderWide
{
    inner Wide
}
`

const listTotalProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const total = 12

// sum is the kept elements' byte total of one list, read through TextAt.
func sum(t *testing.T, v *L, field string) (int64, int32) {
	t.Helper()
	n := int64(0)
	count := int32(0)
	for i := int32(0); i < 8; i++ {
		var p []byte
		var ok bool
		switch field {
		case "names":
			if i >= v.NamesCount {
				return n, count
			}
			p, ok = LNamesTextAt(v, i)
		case "maybe":
			if i >= v.MaybeCount {
				return n, count
			}
			p, ok = LMaybeTextAt(v, i)
		case "repos":
			if i >= v.Repos.Count {
				return n, count
			}
			p, ok = LReposTextAt(v, i)
		default:
			if i >= v.Bases.Count {
				return n, count
			}
			p, ok = LBasesTextAt(v, i)
		}
		if ok {
			n += int64(len(p))
		}
		count++
	}
	return n, count
}

func fill(t *testing.T, b *LBuilder) *L {
	t.Helper()
	v := b.GetRoot()
	for _, s := range []string{"abcd", "efgh", "ijkl"} {
		if !LNamesAppend(v, s) {
			t.Fatalf("names append %q within the total", s)
		}
	}
	if !LMaybeAppend(v, "abcdefgh") || !LMaybeAppendAbsent(v) || !LMaybeAppend(v, "ijkl") {
		t.Fatal("maybe append within the total")
	}
	if !LReposAppend(&b.Main, v, "abcdefgh") || !LReposAppend(&b.Main, v, "ijkl") {
		t.Fatal("repos append within the total")
	}
	if !LBasesAppend(&b.Main, v, "abcdefgh") || !LBasesAppendAbsent(&b.Main, v) || !LBasesAppend(&b.Main, v, "") || !LBasesAppend(&b.Main, v, "ijkl") {
		t.Fatal("bases append within the total")
	}
	return v
}

func TestTextListTotal(t *testing.T) {
	if LTableFields[0].Id != WideTableFields[0].Id || LTableFields[3].Id != WideTableFields[3].Id {
		t.Fatal("L and Wide do not share field ids: the cross-build reads below would read nothing")
	}

	// WITHIN THE TOTAL: three forms round-trip
	var b LBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	v := fill(t, &b)
	n := LMeasure(v, &b.Arena)
	if n < 0 {
		t.Fatal("Measure refused a value within the total")
	}
	wire := make([]byte, n)
	if LSave(v, wire, &b.Arena) != n {
		t.Fatal("Save")
	}
	var report TableReport
	got := LLoad(make([]byte, 1<<14), wire, &report)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("wire load %+v", report)
	}
	resave := func(t *testing.T, x *L) []byte {
		t.Helper()
		m := LMeasure(x)
		if m < 0 {
			t.Fatal("Measure refused")
		}
		out := make([]byte, m)
		if LSave(x, out) != m {
			t.Fatal("Save")
		}
		return out
	}
	if !bytes.Equal(resave(t, got), wire) {
		t.Fatal("wire round trip moved")
	}
	for _, f := range []string{"names", "maybe", "repos", "bases"} {
		if s, _ := sum(t, got, f); s != total {
			t.Fatalf("%s: round-tripped sum %d, want %d", f, s, total)
		}
	}
	b64, err := LSaveText(v, &b.Arena)
	if err != nil {
		t.Fatalf("SaveText %v", err)
	}
	if back, err := LLoadText(b64, nil); err != nil || !bytes.Equal(resave(t, back), wire) {
		t.Fatalf("base64 round trip %v", err)
	}
	jn := LToJsonMeasure(got)
	if jn < 0 {
		t.Fatal("ToJsonMeasure refused a value within the total")
	}
	jtext := make([]byte, jn)
	LToJson(got, jtext)
	var jb LBuilder
	jb.Init()
	defer jb.Shutdown()
	report = TableReport{}
	if !LFromJson(&jb, jtext, &report) || report != (TableReport{}) {
		t.Fatalf("FromJson %+v\n%s", report, jtext)
	}
	jw := make([]byte, LMeasure(jb.GetRoot(), &jb.Arena))
	LSave(jb.GetRoot(), jw, &jb.Arena)
	if !bytes.Equal(jw, wire) {
		t.Fatalf("JSON round trip moved the wire\n%s", jtext)
	}

	// THE WRITERS: Append refuses the element that would cross the total
	if LNamesAppend(v, "m") || LMaybeAppend(v, "m") || LReposAppend(&b.Main, v, "m") || LBasesAppend(&b.Main, v, "m") {
		t.Fatal("an element past the total was appended")
	}
	if v.NamesCount != 3 || v.MaybeCount != 3 || v.Repos.Count != 2 || v.Bases.Count != 4 {
		t.Fatalf("a refused append moved a count: %d %d %d %d", v.NamesCount, v.MaybeCount, v.Repos.Count, v.Bases.Count)
	}
	if !LNamesAppend(v, "") || !LMaybeAppendAbsent(v) || !LBasesAppendAbsent(&b.Main, v) {
		t.Fatal("an element adding nothing to the sum was refused")
	}
	var fb LBuilder
	fb.Init()
	defer fb.Shutdown()
	fv := fb.GetRoot()
	if !LNamesAppend(fv, "abcde") || !LNamesAppend(fv, "fghij") || LNamesAppend(fv, "abc") || !LNamesAppend(fv, "ab") || LNamesAppend(fv, "x") || fv.NamesCount != 3 {
		t.Fatalf("names appends around the total: count %d", fv.NamesCount)
	}

	// Save and Measure refuse a value past the total: a bounded list
	saveRefused := func(t *testing.T, what string) {
		t.Helper()
		if LMeasure(v, &b.Arena) >= 0 || LSave(v, make([]byte, 1<<12), &b.Arena) >= 0 {
			t.Fatalf("%s: Save or Measure took a value past the total", what)
		}
		if _, err := LSaveText(v, &b.Arena); err == nil {
			t.Fatalf("%s: SaveText took a value past the total", what)
		}
	}
	at := v.NamesCount
	copy(v.Names[at].Value[:], "zz")
	v.Names[at].ValueLength = 2
	v.NamesCount++
	saveRefused(t, "names")
	v.NamesCount--
	if LMeasure(v, &b.Arena) < 0 {
		t.Fatal("restored value refused")
	}
	// an unbounded list
	p := v.Repos.At(1, &b.Arena)
	copy(p.Value[:], "ijklm")
	p.ValueLength = 5
	saveRefused(t, "repos")
	p.ValueLength = 4
	if LMeasure(v, &b.Arena) < 0 {
		t.Fatal("restored value refused")
	}
	// a ? list: an absent element made present past the total
	at = v.MaybeCount
	v.Maybe[at].ValuePresent = true
	copy(v.Maybe[at].Value[:], "q")
	v.Maybe[at].ValueLength = 1
	v.MaybeCount++
	saveRefused(t, "maybe")
	v.MaybeCount--
	// a nested table past the total refuses the holder's Save
	var hb HolderBuilder
	hb.Init()
	defer hb.Shutdown()
	hv := hb.GetRoot()
	if !LNamesAppend(&hv.Inner, "abcdefgh") || !LNamesAppend(&hv.Inner, "ijkl") {
		t.Fatal("nested append")
	}
	if HolderMeasure(hv, &hb.Arena) < 0 {
		t.Fatal("nested within the total refused")
	}
	copy(hv.Inner.Names[2].Value[:], "z")
	hv.Inner.Names[2].ValueLength = 1
	hv.Inner.NamesCount = 3
	if HolderMeasure(hv, &hb.Arena) >= 0 || HolderSave(hv, make([]byte, 1<<12), &hb.Arena) >= 0 {
		t.Fatal("a nested list past the total was written")
	}
	// ToJson refuses it, a bounded list and an unbounded one
	got.Names[got.NamesCount] = got.Names[0]
	got.NamesCount++
	if LToJsonMeasure(got) >= 0 {
		t.Fatal("ToJsonMeasure took a bounded list past the total")
	}
	got.NamesCount--
	gp := got.Repos.At(1)
	copy(gp.Value[:], "ijklm")
	gp.ValueLength = 5
	if LToJsonMeasure(got) >= 0 {
		t.Fatal("ToJsonMeasure took an unbounded list past the total")
	}
	gp.ValueLength = 4
	if LToJsonMeasure(got) < 0 {
		t.Fatal("restored value refused by ToJsonMeasure")
	}
	// the retaining save
	var retain TableRetain
	report = TableReport{}
	kept := LLoadRetain(make([]byte, 1<<14), wire, &retain, &report)
	if kept == nil || report != (TableReport{}) {
		t.Fatalf("retain load %+v", report)
	}
	if LSaveRetain(kept, &retain, make([]byte, 1<<12), &report) < 0 {
		t.Fatal("retaining save refused a value within the total")
	}
	kept.Names[kept.NamesCount] = kept.Names[0]
	kept.NamesCount++
	if LSaveRetain(kept, &retain, make([]byte, 1<<12), &report) >= 0 {
		t.Fatal("the retaining save took a value past the total")
	}

	// THE READERS: a wire past the total, written by Wide, which declares none
	cases := []struct {
		field string
		write func(w *WideBuilder, x *Wide) bool
		count int32
	}{
		{"names", func(w *WideBuilder, x *Wide) bool {
			return WideNamesAppend(x, "abcde") && WideNamesAppend(x, "fghij") && WideNamesAppend(x, "k") && WideNamesAppend(x, "lmno") && WideNamesAppend(x, "x")
		}, 3},
		{"maybe", func(w *WideBuilder, x *Wide) bool {
			return WideMaybeAppend(x, "abcdefgh") && WideMaybeAppendAbsent(x) && WideMaybeAppend(x, "ijkl") && WideMaybeAppend(x, "m")
		}, 3},
		{"repos", func(w *WideBuilder, x *Wide) bool {
			return WideReposAppend(&w.Main, x, "abcdefgh") && WideReposAppend(&w.Main, x, "ijkl") && WideReposAppend(&w.Main, x, "m")
		}, 2},
		{"bases", func(w *WideBuilder, x *Wide) bool {
			return WideBasesAppendAbsent(&w.Main, x) && WideBasesAppend(&w.Main, x, "abcdefgh") && WideBasesAppend(&w.Main, x, "ijkl") && WideBasesAppend(&w.Main, x, "") && WideBasesAppend(&w.Main, x, "m")
		}, 4},
	}
	for _, c := range cases {
		var w WideBuilder
		w.Init()
		x := w.GetRoot()
		if !c.write(&w, x) {
			t.Fatalf("%s: Wide append", c.field)
		}
		over := make([]byte, WideMeasure(x, &w.Arena))
		if WideSave(x, over, &w.Arena) != int64(len(over)) {
			t.Fatalf("%s: Wide save", c.field)
		}
		w.Shutdown()
		name := "L." + c.field
		check := func(t *testing.T, how string, got *L, report TableReport) {
			t.Helper()
			if got == nil || report.Clamped != 1 || report.Malformed || report.Reason != "clamped: "+name {
				t.Fatalf("%s %s: %+v", c.field, how, report)
			}
			s, count := sum(t, got, c.field)
			if count != c.count || s > total {
				t.Fatalf("%s %s: kept %d elements summing %d, want %d within %d", c.field, how, count, s, c.count, total)
			}
			if err := report.Err(); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s %s: report.Err %v", c.field, how, err)
			}
		}
		report = TableReport{}
		check(t, "wire", LLoadTolerant(make([]byte, 1<<14), over, &report), report)
		if _, err := LReadStrict(over); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: ReadStrict %v", c.field, err)
		}
		var retain TableRetain
		report = TableReport{}
		check(t, "retain", LLoadRetain(make([]byte, 1<<14), over, &retain, &report), report)
		// base64 is the binary wire
		if _, err := LLoadText(base64.StdEncoding.EncodeToString(over), nil); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: LoadText %v", c.field, err)
		}
	}

	// a nested table past the total
	var nw HolderWideBuilder
	nw.Init()
	defer nw.Shutdown()
	nx := nw.GetRoot()
	if !WideNamesAppend(&nx.Inner, "abcde") || !WideNamesAppend(&nx.Inner, "fghij") || !WideNamesAppend(&nx.Inner, "k") || !WideNamesAppend(&nx.Inner, "lmno") {
		t.Fatal("nested Wide append")
	}
	nested := make([]byte, HolderWideMeasure(nx, &nw.Arena))
	HolderWideSave(nx, nested, &nw.Arena)
	report = TableReport{}
	hgot := HolderLoadTolerant(make([]byte, 1<<14), nested, &report)
	if hgot == nil || report.Clamped != 1 || report.Reason != "clamped: L.names" || hgot.Inner.NamesCount != 3 {
		t.Fatalf("nested read %+v", report)
	}
	if _, err := HolderReadStrict(nested); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), "L.names") {
		t.Fatalf("nested ReadStrict %v", err)
	}

	// THE JSON TEXT past the total: the same prefix kept, counted once, named
	for _, c := range []struct {
		field, text string
		count       int32
	}{
		{"names", "{\"names\":[\"abcde\",\"fghij\",\"k\",\"lmno\",\"x\"]}", 3},
		{"maybe", "{\"maybe\":[\"abcdefgh\",null,\"ijkl\",\"m\"]}", 3},
		{"repos", "{\"repos\":[\"abcdefgh\",\"ijkl\",\"m\"]}", 2},
		{"bases", "{\"bases\":[null,\"abcdefgh\",\"ijkl\",\"\",\"m\"]}", 4},
	} {
		var jb LBuilder
		jb.Init()
		report = TableReport{}
		if !LFromJsonTolerant(&jb, []byte(c.text), &report) || report.Clamped != 1 || report.Malformed || report.Reason != "clamped: L."+c.field {
			t.Fatalf("%s FromJson %+v", c.field, report)
		}
		if err := report.Err(); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), "L."+c.field) {
			t.Fatalf("%s JSON report.Err %v", c.field, err)
		}
		jv := jb.GetRoot()
		var count int32
		var spent int64
		for i := int32(0); i < 8; i++ {
			var p []byte
			var ok bool
			switch c.field {
			case "names":
				count = jv.NamesCount
				p, ok = LNamesTextAt(jv, i)
			case "maybe":
				count = jv.MaybeCount
				p, ok = LMaybeTextAt(jv, i)
			case "repos":
				count = jv.Repos.Count
				p, ok = LReposTextAt(jv, i, &jb.Arena)
			default:
				count = jv.Bases.Count
				p, ok = LBasesTextAt(jv, i, &jb.Arena)
			}
			if ok {
				spent += int64(len(p))
			}
		}
		if count != c.count || spent > total {
			t.Fatalf("%s JSON: kept %d elements summing %d, want %d within %d", c.field, count, spent, c.count, total)
		}
		// what the JSON read kept is a value every writer takes
		if LMeasure(jv, &jb.Arena) < 0 {
			t.Fatalf("%s JSON: the kept prefix is refused by Measure", c.field)
		}
		jb.Shutdown()
	}
}
`
