package gotable

import (
	"strings"
	"testing"
)

// TestGoMapPresence holds docs/SPEC-TABLES.md §2.8's OPTIONAL MAP, read
// through §2.3, in the Go backend, through generated units compiled and run in
// a t.TempDir():
//
//   - `tags ?map[string(8)]int32` in a variable table carries a TagsPresent
//     companion, and ABSENT, PRESENT-EMPTY and PRESENT-WITH-ENTRIES are three
//     values: each round-trips the binary wire, base64 (§16.8) and the JSON
//     text, and each reads back distinct from the other two;
//   - the wire of each is pinned byte for byte: absent writes nothing for the
//     field, present-empty writes the field header and the two-byte array body
//     `0d 00` under L = 2, and present-with-entries is the plain map's bytes;
//   - the JSON of absent omits the key, of present-empty is `{}`, and a `null`
//     reads as absent;
//   - retain-unknown (§6.6) and the cooked form (§7) keep the presence, and
//     the view's descriptor names the field optional with its present offset;
//   - a reader built WITHOUT the `?` reads the present-empty wire as an empty
//     map, the entries wire as its entries, and the absent wire as empty, each
//     silently (§2.8: one framing with two spellings).
func TestGoMapPresence(t *testing.T) {
	t.Parallel()
	runGenerated(t, mapPresenceSchema, mapPresenceProbe)
	runGenerated(t, mapPresencePlainSchema, strings.Replace(mapPresencePlainProbe, "GOLDENS", mapPresenceGoldens, 1))
}

const mapPresenceSchema = `package probe

table M
{
    id   int32
    tags ?map[string(8)]int32
}
`

// mapPresencePlainSchema is the same table without the `?`: the same field
// names, so the same wire ids, the reader §2.8 says reads the optional
// writer's bytes as a plain map.
const mapPresencePlainSchema = `package probe

table M
{
    id   int32
    tags map[string(8)]int32
}
`

// mapPresenceGoldens are the pinned wires, hand-checked against §3 and §2.8.
// Each is the form byte 01, the root body, the zero reference ending it, then
// the id table (one eight-byte id per local id, then the eight-byte count).
// In the body, 01 04 01000000 is `id` (local id 1, kind 4, int32 1). `tags`
// is local id 2, kind 0e (14, the array):
//
//   - absent: no `tags` bytes at all, and the id table names `id` alone;
//   - empty: 02 0e 02 0d 00, the header, L = 2, element kind 13, count 0;
//   - entries: 02 0e 0e 0d 01, L = 14 and one entry, then the entry of
//     L = 11: `key` (local id 3, kind 12, L = 1, "a"), `value` (local id 4,
//     kind 4, int32 5) and its 00; these are the plain map's bytes.
const mapPresenceGoldens = `
const goldenAbsent = "01" + "010401000000" + "00" + "c03a5cb5072eb708" + "0100000000000000"
const goldenEmpty = "01" + "010401000000" + "020e020d00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "0200000000000000"
const goldenEntries = "01" + "010401000000" + "020e0e0d01" + "0b" + "030c0161" + "040405000000" + "00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "ec105b36194ac93d" + "ea0ce83094fde47c" + "0400000000000000"
`

const mapPresenceProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"unsafe"
)

func compact(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func toJson(t *testing.T, v *M) []byte {
	t.Helper()
	n := MToJsonMeasure(v)
	if n < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if MToJson(v, buf) != n {
		t.Fatal("ToJson")
	}
	return buf
}

func saveIn(t *testing.T, v *M) []byte {
	t.Helper()
	n := MMeasure(v)
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	if MSave(v, buf) != n {
		t.Fatal("Save")
	}
	return buf
}

// build makes each of the three values in a builder of its own and locks it.
func build(t *testing.T, b *MBuilder, shape string) *M {
	t.Helper()
	if !b.Init() {
		t.Fatal("init")
	}
	v := b.GetRoot()
	v.Id = 1
	switch shape {
	case "empty":
		v.TagsPresent = true
	case "entries":
		v.TagsPresent = true
		p := MTagsInsert(&b.Main, &v.Tags, "a")
		if p == nil {
			t.Fatal("insert")
		}
		*p = 5
	}
	if !b.Lock() {
		t.Fatal("lock")
	}
	return b.AsConst()
}

// describe reads a loaded value back as the one thing it says.
func describe(v *M, arena *TableArena) string {
	if !v.TagsPresent {
		if v.Tags.Count != 0 {
			return "absent-with-entries"
		}
		return "absent"
	}
	if v.Tags.Count == 0 {
		return "empty"
	}
	var p *int32
	if arena != nil {
		p = MTagsFind(arena, &v.Tags, "a")
	} else {
		p = MTagsFind(nil, &v.Tags, "a")
	}
	if v.Tags.Count != 1 || p == nil || *p != 5 {
		return "entries-wrong"
	}
	return "entries"
}

func load(t *testing.T, wire []byte) *M {
	t.Helper()
	var report TableReport
	got := MLoad(make([]byte, 1<<16), wire, &report)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("load %x: %+v", wire, report)
	}
	return got
}

` + mapPresenceGoldens + `

func TestMapPresence(t *testing.T) {
	cases := []struct{ shape, golden, json string }{
		{"absent", goldenAbsent, "{\"id\":1}"},
		{"empty", goldenEmpty, "{\"id\":1,\"tags\":{}}"},
		{"entries", goldenEntries, "{\"id\":1,\"tags\":{\"a\":5}}"},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		var b MBuilder
		v := build(t, &b, tc.shape)
		wire := saveIn(t, v)
		// THE PINNED WIRE (§2.8)
		if got := hex.EncodeToString(wire); got != tc.golden {
			t.Errorf("%s: wire\n got %s\nwant %s", tc.shape, got, tc.golden)
		}
		if prior, dup := seen[string(wire)]; dup {
			t.Errorf("%s and %s write the same bytes", prior, tc.shape)
		}
		seen[string(wire)] = tc.shape
		// THE BINARY WIRE round trip, read back as the same one of three
		got := load(t, wire)
		if d := describe(got, nil); d != tc.shape {
			t.Errorf("%s: wire load reads back %s", tc.shape, d)
		}
		if again := saveIn(t, got); !bytes.Equal(again, wire) {
			t.Errorf("%s: wire round trip\n%x\n%x", tc.shape, again, wire)
		}
		var lb MBuilder
		lb.Init()
		var report TableReport
		if !MLoadBuilder(&lb, wire, &report) || report != (TableReport{}) {
			t.Fatalf("%s: builder load %+v", tc.shape, report)
		}
		if d := describe(lb.GetRoot(), &lb.Arena); d != tc.shape {
			t.Errorf("%s: builder load reads back %s", tc.shape, d)
		}
		lb.Shutdown()
		// BASE64 (§16.8)
		b64, err := MSaveText(got)
		if err != nil || b64 != base64.StdEncoding.EncodeToString(wire) {
			t.Fatalf("%s: SaveText %q %v", tc.shape, b64, err)
		}
		back, err := MLoadText(b64, nil)
		if err != nil || describe(back, nil) != tc.shape || !bytes.Equal(saveIn(t, back), wire) {
			t.Errorf("%s: LoadText %v", tc.shape, err)
		}
		// THE JSON TEXT (§16.2): absent omits the key, empty is {}
		jtext := toJson(t, got)
		if c := compact(t, jtext); c != tc.json {
			t.Errorf("%s: ToJson %s want %s", tc.shape, c, tc.json)
		}
		var jb MBuilder
		jb.Init()
		report = TableReport{}
		if !MFromJson(&jb, jtext, &report) || report != (TableReport{}) {
			t.Fatalf("%s: FromJson %+v", tc.shape, report)
		}
		if d := describe(jb.GetRoot(), &jb.Arena); d != tc.shape {
			t.Errorf("%s: FromJson reads back %s", tc.shape, d)
		}
		if !jb.Lock() || !bytes.Equal(saveIn(t, jb.AsConst()), wire) {
			t.Errorf("%s: JSON round trip moved the wire", tc.shape)
		}
		jb.Shutdown()
		// RETAIN-UNKNOWN (§6.6) keeps the presence: the same bytes back
		var retain TableRetain
		report = TableReport{}
		kept := MLoadRetain(make([]byte, 1<<16), wire, &retain, &report)
		if kept == nil || report != (TableReport{}) || describe(kept, nil) != tc.shape {
			t.Fatalf("%s: retain load %+v", tc.shape, report)
		}
		rbuf := make([]byte, len(wire)+64)
		if n := MSaveRetain(kept, &retain, rbuf, &report); n < 0 || !bytes.Equal(rbuf[:n], wire) {
			t.Errorf("%s: retain round trip %d\n%x\n%x", tc.shape, n, rbuf[:max(n, 0)], wire)
		}
		// THE COOKED FORM (§7.2) keeps the present bool after the slot
		cn := MCookMeasure(got)
		if cn < 0 {
			t.Fatalf("%s: cook measure refused", tc.shape)
		}
		cooked := make([]byte, cn+64)
		at := 0
		for uintptr(unsafe.Pointer(&cooked[at]))%64 != 0 {
			at++
		}
		cooked = cooked[at : at+int(cn)]
		if !MCookFrom(got, cooked, TableByteOrderLittle) {
			t.Fatalf("%s: cook", tc.shape)
		}
		var cook MCook
		if !MOpen(&cook, unsafe.Pointer(&cooked[0]), cn) {
			t.Fatalf("%s: cook open", tc.shape)
		}
		if d := describe((*M)(cook.Region), nil); d != tc.shape {
			t.Errorf("%s: cooked reads back %s", tc.shape, d)
		}
		b.Shutdown()
	}
	// A JSON null IS ABSENT (§16.2), and writes the absent wire
	var nb MBuilder
	nb.Init()
	defer nb.Shutdown()
	var report TableReport
	if !MFromJson(&nb, []byte("{\"id\":1,\"tags\":null}"), &report) || report != (TableReport{}) {
		t.Fatalf("null FromJson %+v", report)
	}
	if d := describe(nb.GetRoot(), &nb.Arena); d != "absent" {
		t.Fatalf("null reads back %s", d)
	}
	if !nb.Lock() {
		t.Fatal("null lock")
	}
	if got := hex.EncodeToString(saveIn(t, nb.AsConst())); got != goldenAbsent {
		t.Fatalf("null does not write the absent wire\n got %s\nwant %s", got, goldenAbsent)
	}
	// a repeated key whose last value is null leaves the map absent and empty
	var rb MBuilder
	rb.Init()
	defer rb.Shutdown()
	report = TableReport{}
	if !MFromJson(&rb, []byte("{\"id\":1,\"tags\":{\"a\":5},\"tags\":null}"), &report) || report.Duplicate != 1 {
		t.Fatalf("repeated null FromJson %+v", report)
	}
	if d := describe(rb.GetRoot(), &rb.Arena); d != "absent" {
		t.Fatalf("repeated null reads back %s", d)
	}
	// THE VIEW (§8.1): the descriptor names the field optional, its present
	// companion where the struct holds it
	f := &MTableType().Fields[1]
	if f.Name != "tags" || !f.Map || !f.Optional || uintptr(f.PresentOffset) != unsafe.Offsetof(M{}.TagsPresent) {
		t.Fatalf("descriptor %+v", *f)
	}
}
`

// mapPresencePlainProbe is the reader built WITHOUT the `?` (§2.8).
const mapPresencePlainProbe = `package probe

import (
	"encoding/hex"
	"testing"
)

GOLDENS

func TestPlainReader(t *testing.T) {
	for _, tc := range []struct {
		name, golden string
		count       int32
	}{{"absent", goldenAbsent, 0}, {"empty", goldenEmpty, 0}, {"entries", goldenEntries, 1}} {
		wire, _ := hex.DecodeString(tc.golden)
		var report TableReport
		got := MLoad(make([]byte, 1<<16), wire, &report)
		if got == nil || report != (TableReport{}) {
			t.Fatalf("%s: plain load %+v", tc.name, report)
		}
		if got.Id != 1 || got.Tags.Count != tc.count {
			t.Fatalf("%s: plain reader read id %d count %d", tc.name, got.Id, got.Tags.Count)
		}
		if tc.count == 1 {
			if p := MTagsFind(nil, &got.Tags, "a"); p == nil || *p != 5 {
				t.Fatalf("%s: plain reader lost the entry", tc.name)
			}
		}
	}
	// THE PLAIN WRITER: an empty plain map elides, so it writes the absent
	// wire, and a plain map with the entry writes the optional map's bytes
	for _, tc := range []struct {
		name, golden string
		entry       bool
	}{{"empty", goldenAbsent, false}, {"entries", goldenEntries, true}} {
		var b MBuilder
		b.Init()
		v := b.GetRoot()
		v.Id = 1
		if tc.entry {
			*MTagsInsert(&b.Main, &v.Tags, "a") = 5
		}
		if !b.Lock() {
			t.Fatal("lock")
		}
		c := b.AsConst()
		buf := make([]byte, MMeasure(c))
		if MSave(c, buf) != int64(len(buf)) {
			t.Fatal("save")
		}
		if got := hex.EncodeToString(buf); got != tc.golden {
			t.Errorf("%s: plain writer\n got %s\nwant %s", tc.name, got, tc.golden)
		}
		b.Shutdown()
	}
}
`
