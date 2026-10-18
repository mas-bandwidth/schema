package gotable

import (
	"testing"
)

// TestGoJsonLegacyOptions holds docs/SPEC-TABLES.md §16.2's three options in
// the Go backend — null as empty on read, omit defaults and compact on write —
// through a generated unit compiled and run in a t.TempDir():
//
//   - {"names":null,"kids":null,"n":3} reads clean, every counter zero, under
//     NullAsEmpty, and stays two kind mismatches without it, through the plain
//     call and through the With call at the zero options value alike;
//   - the same holds for a map and a bounded array, and a null ELEMENT inside a
//     list keeps its own row's verdict under the option;
//   - the zero value writes {} under OmitDefaults+Compact and {"n":3} with
//     n = 3; an array element is never elided, though its fields are;
//   - encoding/json's own output of the equivalent Go struct reads back equal
//     under all three, and encoding/json reads every option combination's text
//     back equal;
//   - the default text is unchanged: the plain calls and the With calls at the
//     zero options value write the pinned BASE text byte for byte.
func TestGoJsonLegacyOptions(t *testing.T) {
	t.Parallel()
	runGenerated(t, jsonLegacySchema, jsonLegacyProbe)
	t.Run("required", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, jsonLegacyRequiredSchema, jsonLegacyRequiredProbe)
	})
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, jsonLegacyFixedSchema, jsonLegacyFixedProbe)
	})
}

// jsonLegacyFixedSchema is a FIXED table, whose JSON calls take the value
// itself and run tableJsonRead / tableJsonWrite, not the builder path the
// variable tables above use (docs/SPEC-TABLES.md §16.2 holds for both).
const jsonLegacyFixedSchema = `package probe

fixed table F
{
    slots [..3]int32
    n int32
}
`

const jsonLegacyFixedProbe = `package probe

import "testing"

var legacy = TableJsonOptions{NullAsEmpty: true, OmitDefaults: true, Compact: true}

func writeF(t *testing.T, v *F, options TableJsonOptions) string {
	t.Helper()
	n := FToJsonMeasureWith(v, options)
	if n < 0 {
		t.Fatal("ToJsonMeasureWith refused")
	}
	buf := make([]byte, n)
	if got := FToJsonWith(v, buf, options); got != n {
		t.Fatalf("ToJsonWith wrote %d, measure said %d", got, n)
	}
	return string(buf)
}

func TestGoJsonLegacyOptions(t *testing.T) {
	t.Parallel()
	const nullText = "{\"slots\":null,\"n\":3}"
	var v F
	FReset(&v)
	var r TableReport
	if !FFromJsonWith(&v, []byte(nullText), &r, TableJsonOptions{}) || r.KindMismatch != 1 {
		t.Fatalf("without NullAsEmpty: report %+v, want kind_mismatch 1", r)
	}
	FReset(&v)
	r = TableReport{}
	if !FFromJsonWith(&v, []byte(nullText), &r, legacy) || r != (TableReport{}) || v.N != 3 || v.SlotsCount != 0 {
		t.Fatalf("NullAsEmpty: report %+v n %d slots %d", r, v.N, v.SlotsCount)
	}
	if got := writeF(t, &v, legacy); got != "{\"n\":3}" {
		t.Fatalf("n = 3 under OmitDefaults+Compact writes %q", got)
	}
	var zero F
	FReset(&zero)
	if got := writeF(t, &zero, legacy); got != "{}" {
		t.Fatalf("zero under OmitDefaults+Compact writes %q", got)
	}
	plain := make([]byte, FToJsonMeasure(&zero))
	if FToJson(&zero, plain) != int64(len(plain)) || writeF(t, &zero, TableJsonOptions{}) != string(plain) {
		t.Fatal("the zero options value does not write the plain text")
	}
	for _, o := range []TableJsonOptions{{OmitDefaults: true}, {Compact: true}, legacy} {
		var back F
		FReset(&back)
		r = TableReport{}
		if !FFromJsonWith(&back, []byte(writeF(t, &v, o)), &r, TableJsonOptions{}) || r != (TableReport{}) || back != v {
			t.Fatalf("options %+v: does not read back equal: report %+v", o, r)
		}
	}
}
`

// jsonLegacyRequiredSchema holds a field marked required (docs/SPEC-TABLES.md
// §2.3) at the root and inside an array element. Omit defaults never elides
// one (§16.2): a text that leaves it out is refused as missing_required, so the
// write would not read back. A nested table wholly at its default (one) is
// still elided whole: its key absent reads as its defaults, never as missing.
const jsonLegacyRequiredSchema = `package probe

table Inner
{
    y int32 | required
    z int32
}

table R
{
    a int32 | required
    b int32
    kids []Inner
    one Inner
}
`

const jsonLegacyRequiredProbe = `package probe

import "testing"

var legacy = TableJsonOptions{NullAsEmpty: true, OmitDefaults: true, Compact: true}

func readR(t *testing.T, text string) (*R, TableReport, bool) {
	t.Helper()
	b := &RBuilder{}
	if !b.Init() {
		t.Fatal("Init")
	}
	var r TableReport
	ok := RFromJsonWith(b, []byte(text), &r, TableJsonOptions{})
	if !b.Lock() {
		t.Fatal("Lock")
	}
	return b.AsConst(), r, ok
}

func writeR(t *testing.T, v *R, options TableJsonOptions) string {
	t.Helper()
	n := RToJsonMeasureWith(v, options)
	if n < 0 {
		t.Fatal("ToJsonMeasureWith refused")
	}
	buf := make([]byte, n)
	if got := RToJsonWith(v, buf, options); got != n {
		t.Fatalf("ToJsonWith wrote %d, measure said %d", got, n)
	}
	return string(buf)
}

func TestGoJsonLegacyOptions(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"{\"a\":0}", "{\"a\":0}"},
		{"{\"a\":0,\"kids\":[{\"y\":0},{\"y\":2,\"z\":3}]}", "{\"a\":0,\"kids\":[{\"y\":0},{\"y\":2,\"z\":3}]}"},
		{"{\"a\":5,\"b\":0}", "{\"a\":5}"},
	} {
		v, r, ok := readR(t, c.in)
		if !ok || r.MissingRequired != 0 {
			t.Fatalf("read %s: ok %v report %+v", c.in, ok, r)
		}
		for _, o := range []TableJsonOptions{{OmitDefaults: true}, legacy} {
			out := writeR(t, v, o)
			if o == legacy && out != c.want {
				t.Fatalf("%s under OmitDefaults+Compact writes %q, want %q (a required field is always written)", c.in, out, c.want)
			}
			back, r2, ok2 := readR(t, out)
			if !ok2 || r2.MissingRequired != 0 || r2.KindMismatch != 0 || r2.Unknown != 0 {
				t.Fatalf("options %+v: %q does not read back: ok %v report %+v", o, out, ok2, r2)
			}
			if writeR(t, back, TableJsonOptions{}) != writeR(t, v, TableJsonOptions{}) {
				t.Fatalf("options %+v: %q reads back to another instance", o, out)
			}
		}
	}
}
`

const jsonLegacySchema = `package probe

table Child
{
    x int32
}

table L
{
    names []string(8)
    kids []Child
    note *string
    n int32
}

table M
{
    tags map[string(8)]int32
    slots [..3]int32
}
`

const jsonLegacyProbe = `package probe

import (
	"encoding/json"
	"reflect"
	"testing"
)

type legacyChild struct {
	X int32 ` + "`json:\"x\"`" + `
}

type legacyL struct {
	Names []string      ` + "`json:\"names\"`" + `
	Kids  []legacyChild ` + "`json:\"kids\"`" + `
	Note  *string       ` + "`json:\"note\"`" + `
	N     int32         ` + "`json:\"n\"`" + `
}

var legacy = TableJsonOptions{NullAsEmpty: true, OmitDefaults: true, Compact: true}

// baseZeroText is the zero value's text at BASE (sprint/schema-go @
// caea41783528, and unchanged at 9c5fc1244): indented, every field written.
const baseZeroText = "{\n  \"names\": [],\n  \"kids\": [],\n  \"note\": null,\n  \"n\": 0\n}\n"

// readL reads one text into a fresh builder and LOCKS it, so the root a write
// walks is the packed one (an unlocked root is written through its arena)
func readL(t *testing.T, text string, options TableJsonOptions) (*L, TableReport, bool) {
	t.Helper()
	b := &LBuilder{}
	if !b.Init() {
		t.Fatal("Init")
	}
	var r TableReport
	ok := LFromJsonWith(b, []byte(text), &r, options)
	if !b.Lock() {
		t.Fatal("Lock")
	}
	return b.AsConst(), r, ok
}

func readM(t *testing.T, text string, options TableJsonOptions) (*M, TableReport, bool) {
	t.Helper()
	b := &MBuilder{}
	if !b.Init() {
		t.Fatal("Init")
	}
	var r TableReport
	ok := MFromJsonWith(b, []byte(text), &r, options)
	if !b.Lock() {
		t.Fatal("Lock")
	}
	return b.AsConst(), r, ok
}

func clean(r TableReport) bool {
	return r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Duplicate == 0 && !r.Malformed
}

func writeL(t *testing.T, v *L, options TableJsonOptions) string {
	t.Helper()
	n := LToJsonMeasureWith(v, options)
	if n < 0 {
		t.Fatal("ToJsonMeasureWith refused")
	}
	buf := make([]byte, n)
	if got := LToJsonWith(v, buf, options); got != n {
		t.Fatalf("ToJsonWith wrote %d, measure said %d", got, n)
	}
	return string(buf)
}

func plainL(t *testing.T, v *L) string {
	t.Helper()
	n := LToJsonMeasure(v)
	if n < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if LToJson(v, buf) != n {
		t.Fatal("ToJson")
	}
	return string(buf)
}

func normal(v legacyL) legacyL {
	if v.Names == nil {
		v.Names = []string{}
	}
	if v.Kids == nil {
		v.Kids = []legacyChild{}
	}
	return v
}

func marshal(t *testing.T, v legacyL) string {
	t.Helper()
	text, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func TestGoJsonLegacyOptions(t *testing.T) {
	t.Parallel()
	const nullText = "{\"names\":null,\"kids\":null,\"n\":3}"

	// (a) without the option: two kind mismatches, plain call and With alike
	for _, path := range []string{"plain", "with"} {
		b := &LBuilder{}
		if !b.Init() {
			t.Fatal("Init")
		}
		var r TableReport
		var ok bool
		if path == "plain" {
			ok = LFromJson(b, []byte(nullText), &r)
		} else {
			ok = LFromJsonWith(b, []byte(nullText), &r, TableJsonOptions{})
		}
		if !ok || r.KindMismatch != 2 {
			t.Fatalf("%s read of the null text without NullAsEmpty: ok %v kind_mismatch %d, want true 2", path, ok, r.KindMismatch)
		}
		b.Shutdown()
	}

	// (a) with the option: clean, and the same instance {"n":3} reads as
	b, r, ok := readL(t, nullText, TableJsonOptions{NullAsEmpty: true})
	if !ok || !clean(r) {
		t.Fatalf("null text under NullAsEmpty: ok %v report %+v, want clean", ok, r)
	}
	want, _, _ := readL(t, "{\"n\":3}", TableJsonOptions{})
	if got, exp := plainL(t, b), plainL(t, want); got != exp {
		t.Fatalf("null text under NullAsEmpty reads as\n%s\nwant\n%s", got, exp)
	}

	// a null ELEMENT keeps its own row's verdict under the option
	if _, r, ok := readL(t, "{\"names\":[\"a\",null]}", legacy); !ok || r.KindMismatch != 1 {
		t.Fatalf("null element under NullAsEmpty: ok %v kind_mismatch %d, want true 1", ok, r.KindMismatch)
	}

	// a map and a bounded array take the same option
	{
		const mText = "{\"tags\":null,\"slots\":null}"
		mb := &MBuilder{}
		if !mb.Init() {
			t.Fatal("Init")
		}
		var r TableReport
		if !MFromJson(mb, []byte(mText), &r) || r.KindMismatch != 2 {
			t.Fatalf("map/bounded null without the option: kind_mismatch %d, want 2", r.KindMismatch)
		}
		mb.Shutdown()
		// a filled value, then the same keys again as null: last-wins empties both
		m, r, ok := readM(t, "{\"tags\":{\"k\":1},\"slots\":[1],\"tags\":null,\"slots\":null}", TableJsonOptions{NullAsEmpty: true})
		if !ok || r.Duplicate != 2 || r.KindMismatch != 0 || r.Unknown != 0 || r.Clamped != 0 || r.Malformed {
			t.Fatalf("map/bounded null under NullAsEmpty: ok %v report %+v, want two duplicates and nothing else", ok, r)
		}
		n := MToJsonMeasureWith(m, legacy)
		buf := make([]byte, n)
		if MToJsonWith(m, buf, legacy) != n || string(buf) != "{}" {
			t.Fatalf("map/bounded after a null read writes %q, want {} (last-wins emptied both)", buf)
		}
		filled, _, _ := readM(t, "{\"tags\":{\"k\":1},\"slots\":[1]}", TableJsonOptions{})
		n = MToJsonMeasureWith(filled, legacy)
		buf = make([]byte, n)
		if MToJsonWith(filled, buf, legacy) != n || string(buf) != "{\"tags\":{\"k\":1},\"slots\":[1]}" {
			t.Fatalf("filled map/bounded writes %q", buf)
		}
	}

	// (b)+(c): the zero value is {} and n = 3 is {"n":3}
	var zero L
	if got := writeL(t, &zero, TableJsonOptions{OmitDefaults: true, Compact: true}); got != "{}" {
		t.Fatalf("zero value under OmitDefaults+Compact: %q, want {}", got)
	}
	three, _, _ := readL(t, "{\"n\":3}", TableJsonOptions{})
	if got := writeL(t, three, legacy); got != "{\"n\":3}" {
		t.Fatalf("n = 3 under OmitDefaults+Compact: %q, want {\"n\":3}", got)
	}
	// each write option alone
	if got := writeL(t, &zero, TableJsonOptions{OmitDefaults: true}); got != "{}\n" {
		t.Fatalf("zero value under OmitDefaults: %q", got)
	}
	if got := writeL(t, &zero, TableJsonOptions{Compact: true}); got != "{\"names\":[],\"kids\":[],\"note\":null,\"n\":0}" {
		t.Fatalf("zero value under Compact: %q", got)
	}
	// an array element is positional and never elided; its fields are
	kid, _, _ := readL(t, "{\"kids\":[{\"x\":0},{\"x\":2}],\"note\":\"a b\"}", TableJsonOptions{})
	if got := writeL(t, kid, legacy); got != "{\"kids\":[{},{\"x\":2}],\"note\":\"a b\"}" {
		t.Fatalf("elements under OmitDefaults+Compact: %q", got)
	}

	// the default text is unchanged, through the plain and the zero With calls
	if got := plainL(t, &zero); got != baseZeroText {
		t.Fatalf("plain ToJson of the zero value changed:\n%q\nwant\n%q", got, baseZeroText)
	}
	if got := writeL(t, &zero, TableJsonOptions{}); got != baseZeroText {
		t.Fatalf("ToJsonWith at zero options changed:\n%q\nwant\n%q", got, baseZeroText)
	}
	full := "{\"names\":[\"a\",\"b c\"],\"kids\":[{\"x\":1}],\"note\":\"hi\",\"n\":3}"
	fb, _, _ := readL(t, full, TableJsonOptions{})
	if plainL(t, fb) != writeL(t, fb, TableJsonOptions{}) {
		t.Fatal("ToJsonWith at zero options differs from ToJson")
	}

	// encoding/json's own output of the equivalent Go struct reads back equal
	// under all three, and encoding/json reads every option combination back
	note := "hi there"
	empty := ""
	for i, v := range []legacyL{
		{},
		{N: 3},
		{Names: []string{}, Kids: []legacyChild{}},
		{Names: []string{"a", "b c"}, Kids: []legacyChild{{X: 1}, {}}, Note: &note, N: -7},
		{Note: &empty},
	} {
		text := marshal(t, v)
		got, r, ok := readL(t, text, legacy)
		if !ok || !clean(r) {
			t.Fatalf("row %d: encoding/json text %s under all three: ok %v report %+v", i, text, ok, r)
		}
		exp, r2, ok2 := readL(t, marshal(t, normal(v)), TableJsonOptions{})
		if !ok2 || !clean(r2) {
			t.Fatalf("row %d: canonical equivalent of %s: ok %v report %+v", i, text, ok2, r2)
		}
		if a, e := plainL(t, got), plainL(t, exp); a != e {
			t.Fatalf("row %d: %s reads as\n%s\nwant\n%s", i, text, a, e)
		}
		for _, o := range []TableJsonOptions{{}, {OmitDefaults: true}, {Compact: true}, legacy} {
			out := writeL(t, got, o)
			var back legacyL
			if err := json.Unmarshal([]byte(out), &back); err != nil {
				t.Fatalf("row %d options %+v: encoding/json refuses %q: %v", i, o, out, err)
			}
			if !reflect.DeepEqual(normal(back), normal(v)) {
				t.Fatalf("row %d options %+v: %q reads back as %+v, want %+v", i, o, out, back, v)
			}
			again, r3, ok3 := readL(t, out, TableJsonOptions{})
			if !ok3 || !clean(r3) || plainL(t, again) != plainL(t, got) {
				t.Fatalf("row %d options %+v: %q does not read back to the same instance: %+v", i, o, out, r3)
			}
		}
	}
}
`
