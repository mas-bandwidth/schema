package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SHARED GOLDEN BYTES (fixedform.go's opening note). This backend's block
// walk is duplicated from the C++ reference's on purpose, so the place a
// disagreement has to show up is here: the vocabulary block and its fnv1a64
// hash for the paired bench's own table, against the constants the reference
// emits for the same type.
//
// A change to either walk that moves one byte moves the hash, and a hash that
// moved is two ports that can no longer read each other's records at all —
// which is why this is an equality on the NUMBER and not a shape check.
func TestFixedLayoutMatchesReference(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")

	w := fixedWalkRoot(st)
	block := fixedLayoutBytes(w.entries)

	// generated/bench/paired/cpp/FixedTableTable.h, emitted by
	// internal/codegen/cpptable/fixedform.go from these same two schemas
	const (
		refEntries   = 75
		refBlockLen  = 4 + refEntries*fixedEntryBytes
		refHash      = uint64(0x32f1c4a302a224eb)
		refBodyBytes = int64(1236)
	)
	if len(w.entries) != refEntries {
		t.Fatalf("block entries = %d, the C++ reference emits %d", len(w.entries), refEntries)
	}
	if len(block) != refBlockLen {
		t.Fatalf("block bytes = %d, the C++ reference emits %d", len(block), refBlockLen)
	}
	if got := fixedLayoutHash(block); got != refHash {
		t.Fatalf("block hash = 0x%016x, the C++ reference emits 0x%016x — the two walks disagree somewhere in the closure", got, refHash)
	}
	if got := fixedTypeBytes(st); got != refBodyBytes {
		t.Fatalf("body bytes = %d, the C++ reference emits %d", got, refBodyBytes)
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

// THE IDENTITY PLAN IS ONE RUN, which is what the reader's own storage being
// the wire's layout buys — the coalescer's best case reached, not a step
// skipped. If a type ever lays out with a gap this stops being true and the
// emitted single-entry plan would be wrong, so it is asserted rather than
// assumed.
func TestFixedImageIsContiguous(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	var sum int64
	for _, f := range st.Fields {
		sum += fixedFieldBytes(f)
	}
	if sum != fixedTypeBytes(st) {
		t.Fatalf("the body image has a gap: fields sum to %d, the type is %d", sum, fixedTypeBytes(st))
	}
}

// A COUNTED ARRAY'S DECODE WALKS THE LIVE COUNT, never the bound. Identity
// copies the whole array into the image (enums are a flat run); clamp and
// ordinal counters live in the projection both paths share, so a loop over
// ArrayBound would count slack. LIVE elements only, slack never.
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
		if strings.Contains(s, "function LiveRootFixedDecode(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no LiveRootFixedDecode in the generated unit")
	}
	body := jsFn(src, "LiveRootFixedDecode")
	if body == "" {
		t.Fatal("LiveRootFixedDecode was not a closed function")
	}
	if !strings.Contains(body, "i < value.GradesCount") {
		t.Fatalf("counted-array decode must walk the live count, not the bound:\n%s", body)
	}
	if strings.Contains(body, "i < 4") {
		t.Fatalf("counted-array decode still walks the declared bound:\n%s", body)
	}
}

// `bytes(N)` RIDES AS A COUNTED ARRAY, so its destination row has to be the
// COUNTED ARRAY's row and not the text row. The two spell the same two numbers
// in the opposite order — an array entry's `dst` is the ELEMENT BASE and its
// `aux` is where the count lands, which is the pair the plan compiler's array
// case reads — so getting them the other way round lands the count in the
// buffer and the first four content bytes in the used length, on every record
// a COMPILED plan reads. The identity plan never noticed, because it is one
// copy of the whole body and reads no row at all.
func TestFixedBytesRowIsTheCountedArrayRow(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	w := fixedWalkRoot(st)

	// BenchMixed carries one of each: `player_name string(15)` is TEXT, whose
	// row is dst = the length, aux = the buffer; `payload bytes(16)` is an
	// ARRAY, whose row is the other way round.
	var text, array *fixedDst
	for i, e := range w.entries {
		switch w.entries[i].note {
		case "player_name":
			text = &w.dst[i]
		case "payload":
			array = &w.dst[i]
		}
		_ = e
	}
	if text == nil || array == nil {
		t.Fatal("the corpus no longer carries both a string(N) and a bytes(N) field")
	}
	if text.aux != text.dst+fixedCountBytes {
		t.Fatalf("string(N): aux = %d, the buffer stands %d past the length at %d",
			text.aux, fixedCountBytes, text.dst)
	}
	if array.counted != 1 {
		t.Fatalf("bytes(N): counted = %d, a used length rides in front of it", array.counted)
	}
	if array.dst != array.aux+fixedCountBytes {
		t.Fatalf("bytes(N): dst = %d and aux = %d — on an ARRAY row dst is the element base and aux is the count, "+
			"so dst must stand %d past aux; spelled the other way round the compiled plan lands the count in the buffer",
			array.dst, array.aux, fixedCountBytes)
	}
	if array.stride != 1 {
		t.Fatalf("bytes(N): stride = %d, its elements are single bytes", array.stride)
	}
}

// TestFixedFormLoadPrefillsThePlanHoles holds Glenn's ruling: prefill the bytes
// the plan does not write. The compiler computes the unwritten ranges; the
// load copies defaults into exactly those; identity's list is empty because
// its plan is one Copy of the whole body. There is no identity flag in the
// record loop — an empty list is what skips the work.
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
		if strings.Contains(s, "function ConfigFixedLoad(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no ConfigFixedLoad in the generated unit")
	}
	load := jsFn(src, "ConfigFixedLoad")
	if load == "" {
		t.Fatal("ConfigFixedLoad was not a closed function")
	}
	if !strings.Contains(load, "TableFixedHoles") {
		t.Error("the load does not compute the plan's unwritten ranges")
	}
	if !strings.Contains(load, "ConfigFixedPrefill") {
		t.Error("the load has no default image to copy into holes")
	}
	if strings.Contains(load, "image.set(ConfigFixedPrefill)") {
		t.Error("the load still prefills the whole image; holes are the bytes the plan does not write")
	}
	i := strings.Index(load, "for (let k = 0; k < n; k++)")
	if i < 0 {
		t.Fatal("the load has no record loop")
	}
	loop := load[i:]
	if strings.Contains(loop, "identity") {
		t.Error("the load loop still branches on identity; an empty hole list is what skips work")
	}
	if !strings.Contains(src, "function TableFixedHoles(") && !strings.Contains(src, "TableFixedHoles") {
		t.Error("the generated unit never names TableFixedHoles")
	}
	runtimeHas := false
	for _, b := range files {
		if strings.Contains(string(b), "export function TableFixedHoles(") {
			runtimeHas = true
			break
		}
	}
	if !runtimeHas {
		t.Error("the fixed runtime is missing TableFixedHoles")
	}
}

// THE GUARD IS COMPARED AT ArgW BYTES, NEVER AS A PREFIX. A one-byte compare
// fires arm 1 on a foreign 0x0101. The C++/IR leftover on #830 was that JS
// still read src[guard] as a single byte; TableFixedTagAt is the C++ twin and
// the ninth lane carries the width the compiler stamps the way C++ stamps
// e.argw. Flavour stays in Meta. Identity is still one COPY of the body —
// the ArgW lane on that entry is 1, which is the width a tag had when this
// field did not exist.
func TestFixedGuardComparedAtArgW(t *testing.T) {
	u := unitFrom(t, `package probe
table Root { n int32 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var src string
	for _, b := range files {
		s := string(b)
		if strings.Contains(s, "function TableFixedRun(") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no TableFixedRun in the generated unit")
	}
	if !strings.Contains(src, "function TableFixedTagAt(") {
		t.Error("the runtime never names TableFixedTagAt")
	}
	if !strings.Contains(src, "const TableFixedLaneArgW = 8;") {
		t.Error("the plan omits the ArgW lane")
	}
	if !strings.Contains(src, "export const TableFixedLanes = 9;") {
		t.Error("a plan entry is nine int32 lanes, not eight")
	}
	if strings.Contains(src, "src[srcAt + guard] !== e[b + TableFixedLaneArg]") {
		t.Error("the run loop still compares the union guard as one byte")
	}
	if !strings.Contains(src, "TableFixedTagAt(src, srcAt + guard, e[b + TableFixedLaneArgW])") {
		t.Error("the run loop does not compare the guard at ArgW")
	}
	if strings.Contains(src, "if (identity)") {
		t.Error("the load grew a second reader; hash chooses the plan and nothing else")
	}
	if !strings.Contains(src, "RootFixedIdentity = new Int32Array([0, 0, 0, 4, 0, -1, 0, 0, 1])") {
		t.Error("the identity plan omitted the ArgW lane (ninth is 1)")
	}
	if !strings.Contains(src, "plan.argw = (theirTag >= 1 && theirTag <= 8) ? theirTag : 1;") {
		t.Error("the compiler does not stamp ArgW from the writer's tag width")
	}
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

// jsFn is the source of one generated function, from its signature to the
// matching close brace, so a pin can name a loop without matching the rest of
// the module.
func jsFn(body, name string) string {
	sig := "function " + name + "("
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

// loadUnit builds a unit from schema files on disk, so this test reads the
// SAME two files the C++ reference's constants were emitted from.
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

// THE OPTIONAL'S BLOCK ROW AND ITS DESTINATION ROW, against the C++
// reference's own constants for the same schema (docs/SPEC-TABLES.md §3.4).
//
// This is the row pair the old leg had NO CHECK on at all: `fixedRefusal` said
// the port refused `?T` while `fixedRoots` filtered on `fixedSupported` alone,
// so the form was emitted with the refusal riding beside it as a comment — a
// writer that put the payload where the present byte belongs and never wrote
// the last byte of the body. A block entry that agrees with the reference is
// what says the layout is the reference's; the hash below is that agreement in
// one number.
func TestFixedOptionalRowsMatchReference(t *testing.T) {
	u := loadUnit(t, "../../../test/tables/FO1.schema")
	st := findTable(t, u, "OptRoot")

	// build/js-fixed-optional/fo1 (internal/codegen/cpptable/fixedform.go from
	// this same schema), and test/js-tables/fixedoptional_corpus.cpp prints them
	const (
		refEntries   = 25
		refHash      = uint64(0x9cf625d9832802bc)
		refBodyBytes = int64(72)
	)
	w := fixedWalkRoot(st)
	block := fixedLayoutBytes(w.entries)
	if len(w.entries) != refEntries {
		t.Fatalf("block entries = %d, the C++ reference emits %d", len(w.entries), refEntries)
	}
	if got := fixedLayoutHash(block); got != refHash {
		t.Fatalf("block hash = 0x%016x, the C++ reference emits 0x%016x", got, refHash)
	}
	if got := fixedTypeBytes(st); got != refBodyBytes {
		t.Fatalf("body bytes = %d, the C++ reference emits %d", got, refBodyBytes)
	}

	// THE OPTIONAL WRAPPER'S ROW: kind 35, one child, and a size that is the
	// payload's plus the present byte. Its `aux` is where the FLAG lands and
	// its `dst` is unused — the payload's own entry carries the payload's
	// offset, one byte past the flag. Spelled the other way round the compiled
	// plan lands the flag where the payload belongs.
	find := func(note string) (fixedLayoutEntry, fixedDst, int) {
		for i := range w.entries {
			if w.entries[i].note == note {
				return w.entries[i], w.dst[i], i
			}
		}
		t.Fatalf("no block entry noted %q", note)
		return fixedLayoutEntry{}, fixedDst{}, -1
	}
	for _, tc := range []struct {
		note        string
		flagAt      int64
		payloadNote string
	}{
		// name(4+12) + plain(4) = 20
		{"opt ?", 20, "opt"},
		// + present(1) + Leaf(4 + 4+8) = 37
		{"num ?", 37, "num"},
		// + present(1) + int32(4) = 42
		{"mark ?", 42, "mark"},
		// THE NESTED BODY'S OWN OFFSETS, not the root's: `wrap` starts at 44
		// and its body puts n(4) first, so the flag stands at 48 in a record —
		// but the ROW says 4, because a row is an offset inside the entry's
		// PARENT and the plan compiler adds the parent's base as it descends
		// (its `myAt`). A row spelled 48 here would land the flag at 92.
		{"leaf ?", 4, "leaf"},
	} {
		e, d, at := find(tc.note)
		if e.kind != fixedKindOptional {
			t.Errorf("%s: kind = %d, the optional wrapper is %d", tc.note, e.kind, fixedKindOptional)
		}
		if e.children != 1 {
			t.Errorf("%s: children = %d, the wrapper has exactly one", tc.note, e.children)
		}
		if d.aux != tc.flagAt {
			t.Errorf("%s: the present flag lands at %d, §3.4's arithmetic puts it at %d", tc.note, d.aux, tc.flagAt)
		}
		// the payload's entry follows IMMEDIATELY, at one byte past the flag
		payload, pd, _ := find(tc.payloadNote)
		if at+1 >= len(w.entries) || w.entries[at+1].note != tc.payloadNote {
			t.Errorf("%s: the wrapper's one child is not the payload", tc.note)
		}
		if payload.size != e.size-fixedPresentBytes {
			t.Errorf("%s: payload size = %d, the wrapper is %d and carries one present byte",
				tc.note, payload.size, e.size)
		}
		if pd.dst != tc.flagAt+fixedPresentBytes && pd.aux != tc.flagAt+fixedPresentBytes {
			t.Errorf("%s: the payload lands at dst %d / aux %d, the present byte puts it at %d",
				tc.note, pd.dst, pd.aux, tc.flagAt+fixedPresentBytes)
		}
	}

	// AND THE PREFILL SAYS ABSENT WITH THE PAYLOAD AT ITS DECLARED DEFAULT:
	// the flag zero, and `Leaf.v = 7` behind it, which is what an absent field
	// reads as and what makes the reference's absent-payload bytes and this
	// port's agree.
	// and the RECORD's own offsets, which is where the prefill lays them:
	// 44 + 4 = 48 for the nested flag, one byte past `wrap.n`
	prefill := fixedPrefillBytes(st)
	if prefill[20] != 0 {
		t.Errorf("prefill[20] = %d, an optional is born ABSENT", prefill[20])
	}
	if prefill[21] != 7 {
		t.Errorf("prefill[21] = %d, `Leaf.v = 7` stands behind the flag", prefill[21])
	}
	if prefill[48] != 0 || prefill[49] != 7 {
		t.Errorf("prefill[48..49] = %d, %d — the nested optional is born absent at its default",
			prefill[48], prefill[49])
	}
}

// A TABLE THE REFUSAL NAMES GETS NO FORM. The two used to disagree —
// `fixedRefusal` named optionals while `fixedRoots` asked `fixedSupported`
// alone — and a module that says in a comment it carries nothing and then
// carries it is worse than either answer on its own.
func TestFixedRootsAndRefusalAgree(t *testing.T) {
	for _, paths := range [][]string{
		{"../../../test/tables/FO1.schema"},
		{"../../../test/tables/P3.schema"},
		{"../../../test/tables/V1.schema"},
		// the pointered unit, whose tables are the REFUSED half of this: a
		// check that only ever saw roots would pass on an emitter that refused
		// nothing at all
		{"../../../tables/pointers/Graph.schema", "../../../tables/pointers/Marks.schema",
			"../../../tables/pointers/Parts.schema"},
	} {
		u := loadUnit(t, paths...)
		path := paths[0]
		sawRefusal := false
		for _, f := range u.Files {
			roots := map[string]bool{}
			for _, st := range fixedRoots(u, f.Tables) {
				roots[st.Name] = true
			}
			for _, st := range f.Tables {
				if st.IsMapEntry() {
					continue
				}
				refused := fixedRefusal(st) != ""
				if refused == roots[st.Name] {
					t.Errorf("%s: table %s is %sa root and %srefused — the two answers must be opposites",
						path, st.Name, map[bool]string{true: "", false: "not "}[roots[st.Name]],
						map[bool]string{true: "", false: "not "}[refused])
				}
				sawRefusal = sawRefusal || refused
			}
		}
		if len(paths) > 1 && !sawRefusal {
			t.Errorf("%s: the pointered unit refused nothing — this check is watching only roots", path)
		}
	}
}

// TestFixedFu1Fu2GenerateOnePath is the generator half of the TEXT-UNDER-AN-ARM
// pin (test/tables/FU1.schema, FU2.schema). The runtime pin lives in
// test/js-tables/fixedform.mjs: FU1 writes "hello" under the union's second
// arm, FU2 reads those bytes through a COMPILED plan because it appends
// `extra`, and both reads go through TableFixedRun. This test holds the
// emitted load to ONE PATH — hash chooses the plan, empty holes skip, no
// identity memcpy, no `if (identity)` door — and that FU2's extra is in the
// image the prefill will land when the writer does not carry it.
func TestFixedFu1Fu2GenerateOnePath(t *testing.T) {
	fu1 := loadUnit(t, "../../../test/tables/FU1.schema")
	fu2 := loadUnit(t, "../../../test/tables/FU2.schema")

	files1, err := Generate(fu1)
	if err != nil {
		t.Fatalf("generate FU1: %v", err)
	}
	files2, err := Generate(fu2)
	if err != nil {
		t.Fatalf("generate FU2: %v", err)
	}

	src1 := string(files1["FU1Table.js"])
	if src1 == "" {
		t.Fatal("FU1 generated no FU1Table.js")
	}
	load1 := jsFn(src1, "FuRootFixedLoad")
	if load1 == "" {
		t.Fatal("FuRootFixedLoad was not a closed function in FU1")
	}
	if !strings.Contains(load1, "TableFixedRun(") {
		t.Error("FU1 load does not pin through TableFixedRun")
	}
	i := strings.Index(load1, "for (let k = 0; k < n; k++)")
	if i < 0 {
		t.Fatal("FU1 load has no record loop")
	}
	loop := load1[i:]
	if strings.Contains(loop, "identity") {
		t.Error("FU1 load loop still branches on identity; hash chooses the plan and an empty hole list is the skip")
	}
	if strings.Contains(load1, "if (identity)") {
		t.Error("FU1 load has an if (identity) door")
	}
	if !strings.Contains(src1, "const FuRootFixedIdentity = new Int32Array([0, 0, 0,") {
		t.Error("FU1 identity plan is not one COPY of the body")
	}
	if !strings.Contains(src1, "TableFixedHoles") {
		t.Error("FU1 load does not compute the plan's unwritten ranges")
	}

	src2 := string(files2["FU2Table.js"])
	if src2 == "" {
		t.Fatal("FU2 generated no FU2Table.js")
	}
	if !strings.Contains(src2, "function FuRootFixedLoad(") {
		t.Fatal("FU2 generated no FuRootFixedLoad")
	}
	home2 := string(files2["Tblfu2Table.js"])
	if home2 == "" {
		t.Fatal("FU2 generated no Tblfu2Table.js")
	}
	if !strings.Contains(home2, "this.Extra") {
		t.Error("FU2's table class does not carry Extra — the field the compiled read must take from the prefill")
	}
	st2 := findTable(t, fu2, "FuRoot")
	prefill := fixedPrefillBytes(st2)
	// extra is the last int32 of FU2's body, declared default 11
	if len(prefill) < 4 {
		t.Fatalf("FU2 prefill is %d bytes", len(prefill))
	}
	got := int32(prefill[len(prefill)-4]) |
		int32(prefill[len(prefill)-3])<<8 |
		int32(prefill[len(prefill)-2])<<16 |
		int32(prefill[len(prefill)-1])<<24
	if got != 11 {
		t.Errorf("FU2 prefill extra = %d, declared default is 11", got)
	}
}
