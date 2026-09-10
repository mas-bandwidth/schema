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
