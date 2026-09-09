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
func TestFixedBlockMatchesReference(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")

	w := fixedWalkRoot(st)
	block := fixedBlockBytes(w.entries)

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
	if got := fixedBlockHash(block); got != refHash {
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
