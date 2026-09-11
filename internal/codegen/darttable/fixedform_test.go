package darttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SHARED ORACLE BYTES (fixedform.go's opening note). This backend's layout
// walk is duplicated from the C++ reference's on purpose, so the place a
// disagreement has to show up is here: the LAYOUT and its fnv1a64 hash for the
// paired bench's own table, against the constants the reference emits for the
// same type.
//
// A change to either walk that moves one byte moves the hash, and a hash that
// moved is two ports that can no longer read each other's records at all —
// which is why this is an equality on the NUMBER and not a shape check. It
// costs no toolchain: `make tables-dart-fixed-form` proves the same thing over
// seven files of real bytes, and this proves it in `go test`.
func TestFixedLayoutMatchesReference(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")

	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)

	// generated/bench/paired/cpp/FixedTableTable.h, emitted by
	// internal/codegen/cpptable/fixedform.go from these same two schemas
	const (
		refEntries   = 75
		refLayoutLen = fixedHeaderBytes + refEntries*fixedEntryBytes
		refHash      = uint64(0x98d3af4e8ceacd29)
		refBodyBytes = int64(1236)
	)
	if len(w.entries) != refEntries {
		t.Fatalf("layout entries = %d, the C++ reference emits %d", len(w.entries), refEntries)
	}
	if len(layout) != refLayoutLen {
		t.Fatalf("layout bytes = %d, the C++ reference emits %d", len(layout), refLayoutLen)
	}
	if got := ir.TableFixedLayoutHash(layout, st); got != refHash {
		t.Fatalf("layout hash = 0x%016x, the C++ reference emits 0x%016x — the two walks disagree somewhere in the closure", got, refHash)
	}
	if got := fixedTypeBytes(st); got != refBodyBytes {
		t.Fatalf("body bytes = %d, the C++ reference emits %d", got, refBodyBytes)
	}
}

// A `bytes(N)` DESTINATION ROW IS AN ARRAY'S: dest the buffer, aux the live
// count. The TEXT row is the other way round (dest the length, aux the buffer),
// and a `bytes(N)` written under that convention hands compileEntry a count
// destination that is the buffer's first four bytes. Identity lands the field
// with the TEXT op and never reads those columns, so only the compiled path
// saw it.
func TestBytesNDstRowIsAnArray(t *testing.T) {
	u := unitFrom(t, `package probe

table Probe
{
    label string(8)
    blob  bytes(6)
    wide  wstring(3)
    marks [..4]int32
}
`)
	st := findTable(t, u, "Probe")
	w := fixedWalkRoot(st)
	row := map[string]fixedDst{}
	for i, e := range w.entries {
		row[e.note] = w.dst[i]
	}
	label, ok := row["label"]
	if !ok {
		t.Fatal("no destination row for label")
	}
	// TEXT convention: dest the length at the field's start, aux the buffer
	if label.dst != 0 || label.aux != 4 || label.counted != 0 || label.arg != fixedTextUtf8 {
		t.Fatalf("label dst row = %+v, want dest=length 0, aux=buffer 4, counted=0, arg=utf8", label)
	}
	blob, ok := row["blob"]
	if !ok {
		t.Fatal("no destination row for blob")
	}
	// ARRAY convention: dest the buffer (after the length), aux the length
	if blob.dst != 16 || blob.aux != 12 || blob.counted != 1 || blob.stride != 1 || blob.arg != fixedTextBytes {
		t.Fatalf("blob dst row = %+v, want dest=buffer 16, aux=length 12, counted=1, stride=1, arg=bytes", blob)
	}
	wide, ok := row["wide"]
	if !ok {
		t.Fatal("no destination row for wide")
	}
	if wide.dst != 22 || wide.aux != 26 || wide.counted != 0 || wide.arg != fixedTextWide {
		t.Fatalf("wide dst row = %+v, want dest=length 22, aux=buffer 26, counted=0, arg=wide", wide)
	}
	marks, ok := row["marks"]
	if !ok {
		t.Fatal("no destination row for marks")
	}
	if marks.dst != 36 || marks.aux != 32 || marks.counted != 1 || marks.stride != 4 {
		t.Fatalf("marks dst row = %+v, want dest=buffer 36, aux=count 32, counted=1, stride=4", marks)
	}
}

// A COUNTED ARRAY'S DECODE WALKS THE LIVE COUNT, never the bound. Identity
// copies the whole array into the image (enums are a flat run); clamp and
// ordinal counters live in the projection both paths share, so a loop over
// ArrayBound would count slack. LIVE elements only, slack never.
func dumpFn(t *testing.T, src, fn string) string {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all string
	for _, b := range files {
		all += string(b)
	}
	i := strings.Index(all, fn)
	if i < 0 {
		t.Fatalf("no %s in generated unit:\n%s", fn, all)
	}
	body := all[i:]
	if j := strings.Index(body[1:], "\nvoid "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

// A COUNTED ARRAY OF WRAPPED string(N)/wstring(N) IS ONE COPY, never a TEXT
// per bound slot. v1 refuses `[1..4]string(8)` by name ("wrap the element in a
// type"); the wrap is Item { label string(8); wide wstring(3) }. Identity
// used to TEXT all four slots, so slack lengths counted. LIVE elements only,
// slack never: the plan copies the run, decode clamps used-lengths behind
// itemsCount. Compiled same-size text is a COPY for the same reason.
func TestCountedStringArrayCopiesWholeRun(t *testing.T) {
	src := `package probe

table Item
{
    label string(8)
    wide  wstring(3)
}

table LiveNested
{
    items [1..4]Item
}
`
	u := unitFrom(t, src)
	plan := fixedIdentityPlan(findTable(t, u, "LiveNested"))
	texts, copies, counts := 0, 0, 0
	for _, e := range plan {
		switch e.op {
		case fixedOpText:
			texts++
		case fixedOpCopy:
			copies++
		case fixedOpCount:
			counts++
		}
	}
	if counts != 1 {
		t.Fatalf("identity plan count entries = %d, want 1 (items)", counts)
	}
	if texts != 0 {
		t.Fatalf("counted array of wrapped strings must COPY the whole run, not TEXT slack: %d text entries", texts)
	}
	if copies != 1 {
		t.Fatalf("identity plan copy entries = %d, want 1 (the four Items as one run)", copies)
	}

	nested := dumpFn(t, src, "void liveNestedFixedDecode(")
	if !strings.Contains(nested, "i < value.itemsCount") {
		t.Fatalf("counted-array decode must walk the live count:\n%s", nested)
	}
	if strings.Contains(nested, "i < 4") {
		t.Fatalf("counted-array decode still walks the declared bound:\n%s", nested)
	}

	item := dumpFn(t, src, "void itemFixedDecode(")
	if !strings.Contains(item, "if (value.labelLength < 0)") {
		t.Fatalf("decode must clamp a live string length, slack never:\n%s", item)
	}
	if !strings.Contains(item, "if (value.wideLength < 0)") {
		t.Fatalf("decode must clamp a live wstring length, slack never:\n%s", item)
	}
	if !strings.Contains(fixedRuntime, "SAME-SIZE TEXT IS A COPY") {
		t.Fatal("compiled same-size string/wstring must COPY, or slack lengths count on that path")
	}
}

func TestDecodeCountedArrayWalksLiveCount(t *testing.T) {
	u := unitFrom(t, `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

table LiveRoot
{
    grades [1..4]Grade
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var src string
	for _, b := range files {
		s := string(b)
		if strings.Contains(s, "void liveRootFixedDecode(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no liveRootFixedDecode in the generated unit")
	}
	body := src
	if i := strings.Index(src, "void liveRootFixedDecode("); i >= 0 {
		body = src[i:]
		if j := strings.Index(body[1:], "\nvoid "); j >= 0 {
			body = body[:j+1]
		}
	}
	if !strings.Contains(body, "i < value.gradesCount") {
		t.Fatalf("counted-array decode must walk the live count, not the bound:\n%s", body)
	}
	if strings.Contains(body, "i < 4") {
		t.Fatalf("counted-array decode still walks the declared bound:\n%s", body)
	}
}

// A COUNTED ARRAY'S WRITE WALKS THE LIVE COUNT, then the template's zeros
// for slack (SPEC §3.4). Decode does not fill unused slots, so writing the
// bound would put constructor defaults on the wire.
func TestWriteCountedArrayWalksLiveCount(t *testing.T) {
	u := unitFrom(t, `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

table LiveRoot
{
    grades [1..4]Grade
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var src string
	for _, b := range files {
		s := string(b)
		if strings.Contains(s, "void liveRootFixedWriteBody(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no liveRootFixedWriteBody in the generated unit")
	}
	body := src
	if i := strings.Index(src, "void liveRootFixedWriteBody("); i >= 0 {
		body = src[i:]
		if j := strings.Index(body[1:], "\nvoid "); j >= 0 {
			body = body[:j+1]
		}
	}
	if !strings.Contains(body, "i < value.gradesCount") {
		t.Fatalf("counted-array write must walk the live count, not the bound:\n%s", body)
	}
	if strings.Contains(body, "i < 4") {
		t.Fatalf("counted-array write still walks the declared bound:\n%s", body)
	}
}

// A STRING, BYTES, OR WSTRING WRITE COPIES LENGTH AND ZEROES SLACK
// (docs/SPEC-TABLES.md §3.4). Writing all N units would put unwritten or
// stale storage onto the wire instead of the used length. Slack is zeroed.
func TestWriteStringAndBytesCopiesLengthAndZeroesSlack(t *testing.T) {
	u := unitFrom(t, `package probe

table SpanRoot
{
    label string(8)
    blob  bytes(6)
    wide  wstring(4)
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var src string
	for _, b := range files {
		s := string(b)
		if strings.Contains(s, "void spanRootFixedWriteBody(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no spanRootFixedWriteBody in the generated unit")
	}
	body := src
	if i := strings.Index(src, "void spanRootFixedWriteBody("); i >= 0 {
		body = src[i:]
		if j := strings.Index(body[1:], "\nvoid "); j >= 0 {
			body = body[:j+1]
		}
	}

	// string(8): copies length bytes, not bound 8; zeroes slack
	if !strings.Contains(body, "bytes.setRange(at + 4, at + 4 + value.labelLength, value.label);") {
		t.Fatalf("string write must copy length bytes:\n%s", body)
	}
	if strings.Contains(body, "bytes.setRange(at + 4, at + 12, value.label);") {
		t.Fatalf("string write still copies declared bound:\n%s", body)
	}
	if !strings.Contains(body, "bytes.fillRange(at + 4 + value.labelLength, at + 12, 0);") {
		t.Fatalf("string write must zero slack:\n%s", body)
	}

	// bytes(6): copies length bytes, not bound 6; zeroes slack
	if !strings.Contains(body, "bytes.setRange(at + 16, at + 16 + value.blobLength, value.blob);") {
		t.Fatalf("bytes write must copy length bytes:\n%s", body)
	}
	if strings.Contains(body, "bytes.setRange(at + 16, at + 22, value.blob);") {
		t.Fatalf("bytes write still copies declared bound:\n%s", body)
	}
	if !strings.Contains(body, "bytes.fillRange(at + 16 + value.blobLength, at + 22, 0);") {
		t.Fatalf("bytes write must zero slack:\n%s", body)
	}

	// wstring(4): loops length code units, not bound 4; zeroes slack
	if !strings.Contains(body, "for (var i = 0; i < value.wideLength; i++) {") {
		t.Fatalf("wstring write must loop length code units:\n%s", body)
	}
	if strings.Contains(body, "for (var i = 0; i < 4; i++) {") {
		t.Fatalf("wstring write still loops declared bound:\n%s", body)
	}
	if !strings.Contains(body, "bytes.fillRange(at + 26 + value.wideLength * 2, at + 34, 0);") {
		t.Fatalf("wstring write must zero slack:\n%s", body)
	}
}

// THE PREFILL IS THE DECLARED DEFAULTS, and the one this corpus carries is
// `has_extra bool = true` — the byte that would silently read false if the
// prefill were a zero fill.
func TestFixedPrefillCarriesDeclaredDefaults(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	prefill := fixedPrefillBytes(st)
	if int64(len(prefill)) != fixedTypeBytes(st) {
		t.Fatalf("prefill is %d bytes, the body is %d", len(prefill), fixedTypeBytes(st))
	}
	// has_extra is the last-but-two field of BenchMixed, at body offset 1227
	if prefill[1227] != 1 {
		t.Fatalf("prefill[1227] = %d, `has_extra bool = true` should stand there", prefill[1227])
	}
	// and the counted array `entities` is born at its declared minimum of 1
	if prefill[52] != 1 {
		t.Fatalf("prefill[52] = %d, `entities [1..8]` is born at its minimum of 1", prefill[52])
	}
}

// TestFixedIdentityCoverIsThePlanAndHolesAreEmpty holds Glenn's ruling: the
// cover IS the identity plan's destinations, so subtracting what that plan
// lands leaves nothing. Empty fill is the skip — identity prefills nothing.
func TestFixedIdentityCoverIsThePlanAndHolesAreEmpty(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	plan := fixedIdentityPlan(st)
	cover := fixedIdentityCover(plan)
	if len(cover) == 0 {
		t.Fatal("identity coverage of FixedTable is empty")
	}
	body := fixedTypeBytes(st)
	if len(cover) != 1 || cover[0].dst != 0 || cover[0].size != body {
		t.Fatalf("identity cover = %v, Dart's image is the wire so the cover is one run of %d", cover, body)
	}
	covered := make([]bool, len(cover))
	for _, e := range plan {
		for _, r := range fixedEntryLands(e) {
			found := false
			for i, c := range cover {
				if r.dst >= c.dst && r.dst+r.size <= c.dst+c.size {
					covered[i] = true
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("plan dest [%d,%d) is not in identity coverage", r.dst, r.dst+r.size)
			}
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("coverage range [%d,%d) is not a plan destination — identity holes must be empty",
				cover[i].dst, cover[i].dst+cover[i].size)
		}
	}
}

// TestFixedFormLoadPrefillsThePlanHoles holds Glenn's ruling: the plan
// compiler prefills exactly the ranges the plan does not land; identity
// prefills nothing. Hash chooses the plan and nothing else. There is no
// identity flag in the record loop — an empty list is what skips the work.
func TestFixedFormLoadPrefillsThePlanHoles(t *testing.T) {
	u := unitFrom(t, `package probe
table Config {
    scale float32 = 1.0
    extra int32 = 9
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var src string
	for _, b := range files {
		s := string(b)
		if strings.Contains(s, "int configFixedLoad(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no configFixedLoad in the generated unit")
	}
	load := dartFn(src, "int configFixedLoad(")
	if load == "" {
		t.Fatal("configFixedLoad was not a closed function")
	}
	if !strings.Contains(load, "tableFixedFillRun") {
		t.Error("the load does not copy the plan's unwritten ranges")
	}
	if !strings.Contains(load, "configFixedPrefill") {
		t.Error("the load has no default image to copy into holes")
	}
	if !strings.Contains(load, "configFixedCover") {
		t.Error("the load does not hand the cover to the plan compiler")
	}
	if !strings.Contains(load, "fillCount = 0") {
		t.Error("identity hole list is not the empty skip")
	}
	if strings.Contains(load, "plan.image.setRange") {
		t.Error("the load still prefills the whole image; holes are the bytes the plan does not land")
	}
	i := strings.Index(load, "for (var k = 0; k < n; k++)")
	if i < 0 {
		t.Fatal("the load has no record loop")
	}
	loop := load[i:]
	if strings.Contains(loop, "identity") {
		t.Error("the load loop still branches on identity; an empty hole list is what skips work")
	}
	runtimeHas := false
	for _, b := range files {
		if strings.Contains(string(b), "void tableFixedFillRun(") &&
			strings.Contains(string(b), "static int fills(") {
			runtimeHas = true
			break
		}
	}
	if !runtimeHas {
		t.Error("the fixed runtime is missing tableFixedFillRun or TableFixedCompiler.fills")
	}
}

func dartFn(body, sig string) string {
	i := strings.Index(body, sig)
	if i < 0 {
		return ""
	}
	n := 0
	for j := i; j < len(body); j++ {
		switch body[j] {
		case '{':
			n++
		case '}':
			n--
			if n == 0 {
				return body[i : j+1]
			}
		}
	}
	return body[i:]
}

// A NAMED string(N) DEFAULT LIVES IN CONSTRUCTED STORAGE, not only in the
// prefill and not only as a used length. FX1's `label string(8) = "fx"` is
// the leftover: C++ still writes `char label[8 + 1] = "fx"`, and Dart used
// to emit `Uint8List(8)` plus `labelLength = 2` — two claimed bytes of NULs.
func TestNamedStringDefaultLivesInStorage(t *testing.T) {
	u := unitFrom(t, `package probe

table Fx1
{
    label string(8) = "fx"
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all string
	for _, b := range files {
		all += string(b)
	}
	if !strings.Contains(all, `"fx"`) {
		t.Fatalf("generated Dart must carry the named default \"fx\":\n%s", all)
	}
	if !strings.Contains(all, "labelLength = 2") {
		t.Fatalf("generated Dart must carry labelLength = 2:\n%s", all)
	}
	if strings.Contains(all, "final Uint8List label = Uint8List(8);") {
		t.Fatalf("construction allocated an empty buffer; \"fx\" is length-only:\n%s", all)
	}
	if !strings.Contains(all, "..[0] = 0x66") || !strings.Contains(all, "..[1] = 0x78") {
		t.Fatalf("construction must lay the bytes of \"fx\" into the buffer:\n%s", all)
	}
}

// THE IDENTITY PLAN IS THE REFERENCE'S LEAF WALK COALESCED, and what this pins
// is the property that makes it correct rather than merely small: EVERY COUNT
// AND EVERY TEXT LENGTH THE TYPE DECLARES HAS ITS OWN ENTRY. Those two are the
// only bytes a record does not merely move — a hostile one has to be clamped
// to this reader's own bound before it reaches a value a consumer indexes with
// — so a plan that coalesced them away would be a plan that lets a forged
// length through, and a Dart consumer would meet it as a RangeError.
func TestFixedIdentityPlanClampsEveryCountAndLength(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	plan := fixedIdentityPlan(st)

	counts, texts := 0, 0
	for _, e := range plan {
		switch e.op {
		case fixedOpCount:
			counts++
		case fixedOpText:
			texts++
		case fixedOpCopy:
		default:
			t.Fatalf("the identity plan carries op %d; only copy, count and text belong in it", e.op)
		}
	}
	// BenchMixed declares two counted arrays (entities, stats) and two text
	// fields (player_name, payload)
	if counts != 2 {
		t.Errorf("the identity plan carries %d count entries; the type declares 2 counted arrays", counts)
	}
	if texts != 2 {
		t.Errorf("the identity plan carries %d text entries; the type declares 2 text fields", texts)
	}

	// AND IT COVERS THE BODY WITH NO GAP. The entries are in source order and
	// each one starts where the last ended, so nothing of the record is left
	// standing at the prefill where a value should have landed. THE ONE PLACE
	// TWO ENTRIES SHARE A SOURCE is a UNION's arms: every arm is laid at the
	// same offset under its own tag guard, and what the record spends there is
	// the WIDEST of them, which is what the walk steps over.
	body := fixedTypeBytes(st)
	var at, widest int64
	for i, e := range plan {
		if e.src != e.dst {
			t.Fatalf("plan entry %d has src %d and dst %d; the identity plan's source IS its destination in this backend",
				i, e.src, e.dst)
		}
		if e.guard != fixedNoGuard {
			// an ARM: it starts where the walk stands and does not advance it
			if e.src != at {
				t.Fatalf("plan entry %d is an arm at %d, the walk is at %d", i, e.src, at)
			}
			if end := planEnd(e) - at; end > widest {
				widest = end
			}
			continue
		}
		if widest != 0 {
			at += widest // the union's widest arm, which the arms shared
			widest = 0
		}
		if e.src != at {
			t.Fatalf("plan entry %d starts at %d, the walk is at %d", i, e.src, at)
		}
		at = planEnd(e)
	}
	at += widest
	if at != body {
		t.Fatalf("the identity plan covers %d bytes, the body is %d", at, body)
	}
}

// planEnd is the record offset one past the bytes an entry accounts for. A
// COPY moves its size; a COUNT's `size` is the reader's own BOUND, so the bytes
// it consumes are the four the count rides in and its elements are the entries
// behind it; a TEXT states its units and the length rides in front of them.
func planEnd(e fixedPlanEntry) int64 {
	switch e.op {
	case fixedOpCount:
		return e.src + fixedCountBytes
	case fixedOpText:
		return e.src + fixedCountBytes + e.size
	default:
		return e.src + e.size
	}
}

// EVERY ROOT OF THE TABLE CORPUS LAYS OUT, and its layout is one this backend's
// own reader accepts: the entry count closes the tree, every size sums, and the
// record is inside §3.4's ceiling. It is the cheap standing check that the walk
// and the validation agree — the two are written from one page and this is
// where they are read against each other.
func TestFixedLayoutsPassTheReadersOwnRules(t *testing.T) {
	u := loadUnit(t,
		"../../../tables/examples/Keyed.schema",
		"../../../tables/examples/Pack.schema",
		"../../../tables/examples/Nested.schema",
		"../../../tables/examples/Ranges.schema",
		"../../../tables/examples/Tables.schema",
		"../../../tables/examples/Guarded.schema",
		"../../../tables/examples/Wide.schema",
	)
	roots := 0
	for _, f := range u.Files {
		for _, st := range fixedRoots(f.Tables) {
			roots++
			w := fixedWalkRoot(st)
			layout := fixedLayoutBytes(w.entries)
			if len(layout) != fixedHeaderBytes+len(w.entries)*fixedEntryBytes {
				t.Errorf("%s: the layout is %d bytes for %d entries", st.Name, len(layout), len(w.entries))
			}
			if len(w.dst) != len(w.entries) {
				t.Errorf("%s: %d dst rows for %d entries — MY side of the layout is one row per entry",
					st.Name, len(w.dst), len(w.entries))
			}
			if got := fixedTypeBytes(st); got > ir.TableFixedRecordMaxBytes {
				t.Errorf("%s: a root past §3.4's ceiling reached fixedRoots: %d bytes", st.Name, got)
			}
			// the ROOT is a table, at kind 13, carrying the record's body size
			if w.entries[0].kind != ir.TableKindTable {
				t.Errorf("%s: entry 0 is kind %d, the root is a TABLE", st.Name, w.entries[0].kind)
			}
			if w.entries[0].size != fixedTypeBytes(st) {
				t.Errorf("%s: entry 0 states %d bytes, the body is %d", st.Name, w.entries[0].size, fixedTypeBytes(st))
			}
			// THE PRE-ORDER WALK CONSUMES EXACTLY THE ENTRIES
			if used := fixedSubtree(w.entries, 0); used != len(w.entries) {
				t.Errorf("%s: the tree closes after %d of %d entries", st.Name, used, len(w.entries))
			}
		}
	}
	if roots == 0 {
		t.Fatal("no fixed root at all in the table corpus — the walk, not the corpus, is what broke")
	}
}

// fixedSubtree is the reader's own walk, in Go, so the test asks the question
// the Dart runtime asks rather than a shape of its own.
func fixedSubtree(entries []fixedLayoutEntry, i int) int {
	if i < 0 || i >= len(entries) {
		return 1
	}
	n, at := 1, i+1
	for c := 0; c < entries[i].children; c++ {
		if at >= len(entries) {
			break
		}
		sub := fixedSubtree(entries, at)
		at += sub
		n += sub
	}
	return n
}

// TestFixedFormFU1FU2OnePath is the TEXT-UNDER-AN-ARM generate pin
// (docs/SPEC-TABLES.md §3.4, §15; docs/FIXED-FORM-ALGORITHM.md §4.1 fix 12).
// FU1 writes a string(8) in the union's SECOND arm; FU2 appends `extra` so a
// read of those bytes is a COMPILED plan. The generated load is the one-path
// load: hash chooses the plan, tableFixedRun walks it, empty fill/holes is
// the skip. No second reader. No `if (identity)` door.
func TestFixedFormFU1FU2OnePath(t *testing.T) {
	fu1 := loadUnit(t, "../../../test/tables/FU1.schema")
	fu2 := loadUnit(t, "../../../test/tables/FU2.schema")
	files1, err := Generate(fu1)
	if err != nil {
		t.Fatalf("FU1 generate: %v", err)
	}
	files2, err := Generate(fu2)
	if err != nil {
		t.Fatalf("FU2 generate: %v", err)
	}
	var all1, all2 string
	for _, b := range files1 {
		all1 += string(b)
	}
	for _, b := range files2 {
		all2 += string(b)
	}
	if !strings.Contains(all1, "int fuRootFixedLoad(") {
		t.Fatal("FU1 did not emit fuRootFixedLoad")
	}
	if !strings.Contains(all2, "int fuRootFixedLoad(") {
		t.Fatal("FU2 did not emit fuRootFixedLoad")
	}
	if strings.Contains(all1, "if (identity)") || strings.Contains(all1, "if identity") {
		t.Error("FU1 load still forks on an identity flag")
	}
	if !strings.Contains(all1, "tableFixedRun(") {
		t.Error("FU1 FixedLoad is not the one-path load")
	}
	if !strings.Contains(all2, "int extra = 11") {
		t.Error("FU2 did not emit extra's declared default")
	}
	h1 := strings.Index(all1, "const int fuRootFixedHash = ")
	h2 := strings.Index(all2, "const int fuRootFixedHash = ")
	if h1 < 0 || h2 < 0 {
		t.Fatal("missing fuRootFixedHash")
	}
	line1 := all1[h1 : h1+80]
	line2 := all2[h2 : h2+80]
	if line1 == line2 {
		t.Fatal("FU2 extra did not change the layout hash; compiled path is not a compile trigger")
	}
}

func findTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("table %s not found in the unit", name)
	return nil
}

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// loadUnit builds a unit from schema files on disk, so this test reads the SAME
// files the C++ reference's constants were emitted from.
func loadUnit(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(filepath.Base(p), ".schema")
		ast, errs := parser.Parse(filepath.Base(p), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", p, errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: filepath.Base(p), Base: base, Bytes: src, AST: ast})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}
