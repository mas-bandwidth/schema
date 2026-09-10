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
