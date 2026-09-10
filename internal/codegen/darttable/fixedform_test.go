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
		refHash      = uint64(0x32f1c4a302a224eb)
		refBodyBytes = int64(1236)
	)
	if len(w.entries) != refEntries {
		t.Fatalf("layout entries = %d, the C++ reference emits %d", len(w.entries), refEntries)
	}
	if len(layout) != refLayoutLen {
		t.Fatalf("layout bytes = %d, the C++ reference emits %d", len(layout), refLayoutLen)
	}
	if got := fixedLayoutHash(layout); got != refHash {
		t.Fatalf("layout hash = 0x%016x, the C++ reference emits 0x%016x — the two walks disagree somewhere in the closure", got, refHash)
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

// THE IDENTITY PLAN IS A SINGLE WHOLE-BODY COPY RUN per Glenn's ruling.
// Clamping of counts and text lengths is done in the decode projection directly
// (with report.clamped accounting), so the identity plan does not need multi-entry
// plan walking.
func TestFixedIdentityPlanIsSingleCopyRun(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	plan := fixedIdentityPlan(st)

	body := fixedTypeBytes(st)
	if len(plan) != 1 {
		t.Fatalf("expected 1 entry in identity plan, got %d", len(plan))
	}
	e := plan[0]
	if e.op != fixedOpCopy || e.src != 0 || e.dst != 0 || e.size != body || e.guard != fixedNoGuard {
		t.Fatalf("unexpected identity plan entry: %+v (expected copy 0..%d)", e, body)
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
