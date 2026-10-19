package gotable

// THE SHARED BYTE ORACLE AND HOSTILE-INPUT ROWS ON THE GO LEG (docs/roadmap.sexp
// node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md
// §5.7 (the corpus and the manifest, "the manifest is the wire", THE WRITE and
// THE READ) and docs/SPEC-TABLES.md §3.4 ("the C++ backend is the REFERENCE for
// this form, so the reference is what writes the bytes and every port matches
// them"). One subtest per leaf this card owns — go/W13, go/P2, go/R27, go/P4 —
// table-driven, t.Parallel() first. P3 is already pinned by
// TestFixedFormHostileByteSweep (fixedform_test.go) and is not duplicated here;
// go/W6 and go/P1 belong to the-plan-is-partitioned-unguarded-first and are not
// this card's.
//
// The bytes are the C++ reference's own corpus: `make tables-fixedform-corpus`
// writes build/fixedform-corpus (fx1.bin and manifest.txt among it), and every
// subtest below gets them through fixedCorpus (fixedversioning_test.go) — the
// same gate the versioning rows use, so a missing corpus is a SKIP on a bare
// `go test ./...` and a FAILURE under SCHEMA_REQUIRE_CORPUS=1 (the make leg
// gate). The manifest is read PER FILE and in DECIMAL, never from the bytes
// under test (§5.9 #32). Each subtest compiles and runs the leg's own emitted
// unit against the reference bytes through t09Run, so the assertion is a byte
// fact, not a spelling one.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedRoadmapGoT12ConformanceHostile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/W13", t12W13},
		{"go/P2", t12P2},
		{"go/R27", t12R27},
		{"go/P4", t12P4},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t12FX1 is the versioning pair's FX1 unit (test/tables/FX1.schema): the root
// FxRoot whose layout, hash and two records the reference's corpus
// (build/fixedform-corpus/fx1.bin) carries. Its package is tblfx1.
func t12FX1(t *testing.T) string {
	t.Helper()
	return readSchema(t, "FX1")
}

// ---- go/W13 -----------------------------------------------------------------

// t12W13 holds W13's clause: "layout+hash == C++ reference". The corpus file
// fx1.bin is the C++ reference's byte oracle; its header carries the
// reference's layout hash at TableFixedHashAt and the layout block behind the
// u32 length at TableFixedHeaderBytes (§2: "8..15 the layout hash; 16..19 the
// layout's LENGTH IN BYTES; 20.. the layout"). The leg's emitted runtime names
// the same two facts as FxRootFixedHash and FxRootFixedLayout. The clause is
// that they are THE SAME BYTES.
func t12W13(t *testing.T) {
	corpus := fixedCorpus(t)
	schema := t12FX1(t)
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`package tblfx1

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestW13(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	if h := binary.LittleEndian.Uint64(data[TableFixedHashAt:]); h != FxRootFixedHash {
		t.Fatalf("W13: the header hash 0x%%016x is not the emitted 0x%%016x", h, FxRootFixedHash)
	}
	lb := binary.LittleEndian.Uint32(data[TableFixedHeaderBytes:])
	if int(lb) != len(FxRootFixedLayout) {
		t.Fatalf("W13: the layout length %%d is not the emitted %%d", lb, len(FxRootFixedLayout))
	}
	if string(data[TableFixedHeaderBytes+4:TableFixedHeaderBytes+4+int(lb)]) != string(FxRootFixedLayout) {
		t.Fatal("W13: the layout bytes differ from the emitted layout")
	}
}
`, file)
	t09Run(t, schema, probe)
}

// ---- go/P2 ------------------------------------------------------------------

// t12P2 holds P2's clause: "write-read-write byte-identical". The reference's
// fx1.bin is read through the ONE plan-driven load path and saved back; the
// emitted bytes must be the file's own bytes, every field included (§5.7's THE
// WRITE: "a byte a port encodes differently is a byte that does not come
// back").
func t12P2(t *testing.T) {
	corpus := fixedCorpus(t)
	schema := t12FX1(t)
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`package tblfx1

import (
	"bytes"
	"os"
	"testing"
)

func TestP2(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]FxRoot, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := FxRootFixedLoad(back, data, plan, &r)
	if n < 0 {
		t.Fatalf("P2: the reference file did not load: n=%%d %%+v", n, r)
	}
	out := make([]byte, FxRootFixedMeasure(n))
	m := FxRootFixedSave(back[:n], out)
	if m != int64(len(data)) {
		t.Fatalf("P2: re-save %%d != file %%d", m, len(data))
	}
	if !bytes.Equal(out, data) {
		t.Fatal("P2: write-read-write differs")
	}
}
`, file)
	t09Run(t, schema, probe)
}

// ---- go/R27 -----------------------------------------------------------------

// t12R27 holds R27's clause: "the manifest is the wire". build/fixedform-corpus/
// manifest.txt is the single value oracle every leg reads to (§5.7 step 1, §5.9
// #32): the dump writes the values it put on the wire into the manifest, and a
// leg asserts THE MANIFEST and never the dump. The probe reads the manifest's
// fx1.bin line, loads the reference file through the leg's own loader, and
// holds every value-for-value against the manifest.
func t12R27(t *testing.T) {
	corpus := fixedCorpus(t)
	schema := t12FX1(t)
	manifest := filepath.Join(corpus, "manifest.txt")
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`package tblfx1

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// manifestNumeric reads ONE FILE'S row out of the corpus manifest and answers
// its values= map, holding only the pairs whose value is a DECIMAL NUMBER. A
// pair whose value is not a number — the quoted label "fx1" — is not a numeric
// oracle and is skipped.
func manifestNumeric(t *testing.T, path string) map[string]uint64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "file=fx1.bin ") {
			continue
		}
		out := map[string]uint64{}
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok || k != "values" {
				continue
			}
			for _, pair := range strings.Split(v, ",") {
				pk, pv, ok := strings.Cut(pair, "=")
				if !ok {
					continue
				}
				if n, err := strconv.ParseUint(pv, 10, 64); err == nil {
					out[pk] = n
				}
			}
		}
		return out
	}
	t.Fatal("R27: the manifest carries no line for fx1.bin")
	return nil
}

func TestR27(t *testing.T) {
	want := manifestNumeric(t, %q)
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]FxRoot, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := FxRootFixedLoad(back, data, plan, &r)
	if n != 2 {
		t.Fatalf("R27: read %%d records, want 2 (%%+v)", n, r)
	}
	if r != (TableReport{}) {
		t.Fatalf("R27: a clean read of the reference bytes is not a refusal: %%+v", r)
	}
	check := func(key string, got uint64) {
		t.Helper()
		w, ok := want[key]
		if !ok {
			t.Fatalf("R27: the manifest carries no %%s", key)
		}
		if w != got {
			t.Errorf("R27: the manifest says %%s=%%d, the wire carries %%d", key, w, got)
		}
	}
	check("r0.keep", uint64(back[0].Keep))
	check("r0.narrow", uint64(back[0].Narrow))
	check("r0.renamed", uint64(back[0].Renamed))
	check("r0.gone", uint64(back[0].Gone))
	check("r0.nested.a", uint64(back[0].Nested.A))
	check("r0.nested.b", uint64(back[0].Nested.B))
	check("r0.label_length", uint64(back[0].LabelLength))
	check("r0.marks_count", uint64(back[0].MarksCount))
	check("r0.marks[0]", uint64(back[0].Marks[0]))
	check("r0.marks[1]", uint64(back[0].Marks[1]))
	check("r0.blob_length", uint64(back[0].BlobLength))
	check("r0.blob[0]", uint64(back[0].Blob[0]))
	check("r1.keep", uint64(back[1].Keep))
	check("r1.narrow", uint64(back[1].Narrow))
	check("r1.renamed", uint64(back[1].Renamed))
	check("r1.gone", uint64(back[1].Gone))
	check("r1.nested.a", uint64(back[1].Nested.A))
	check("r1.nested.b", uint64(back[1].Nested.B))
	check("r1.label_length", uint64(back[1].LabelLength))
	check("r1.marks_count", uint64(back[1].MarksCount))
	check("r1.blob_length", uint64(back[1].BlobLength))
}
`, manifest, file)
	t09Run(t, schema, probe)
}

// ---- go/P4 ------------------------------------------------------------------

// t12P4 holds P4's clause: "wrong plan goes red". Two fixed tables of DIFFERENT
// layouts are emitted into one package; one record is written with A's writer
// and the run loop is given A's plan (which reproduces it) and then B's plan
// (which must not). The clause mirrors the C++ reference's own control
// (internal/codegen/cpptable/fixedroadmap_t08_test.go t08P4, test/tables/
// fixedform_main.cpp case 8): "a reader given the WRONG PLAN for a record must
// come out wrong. A test that never watched the wrong plan fail never checked
// the right one." tools/sabotage is not used.
func t12P4(t *testing.T) {
	schema := `package probe

fixed table A {
    lead  uint32 = 0
    x     int32  = 0
    y     int32  = 0
    trail uint32 = 0
}

fixed table B {
    p int8 = 0
    q int8 = 0
}
`
	probe := `package probe

import (
	"testing"
	"unsafe"
)

func TestP4(t *testing.T) {
	var one A
	AReset(&one)
	one.Lead = 0xAABBCCDD
	one.X = 111
	one.Y = 222
	one.Trail = 0x11223344
	buf := make([]byte, AFixedMeasure(1))
	if n := AFixedSave([]A{one}, buf); n != int64(len(buf)) {
		t.Fatalf("P4: A did not save: %d", n)
	}
	body := buf[len(buf)-AFixedRecordBytes+8:] // the record's own eight-byte hash first

	var good A
	AReset(&good)
	var r TableReport
	dst := tableFixedOverlay(unsafe.Pointer(&good), unsafe.Sizeof(good))
	tableFixedRun(AFixedPlan.Entries, AFixedPlan.Count, body, dst, &r)
	right := good.Lead == 0xAABBCCDD && good.X == 111 && good.Y == 222 && good.Trail == 0x11223344
	if !right {
		t.Fatalf("P4: the RIGHT plan did not reproduce the record: %+v", good)
	}

	var wrong A
	AReset(&wrong)
	var r2 TableReport
	dst2 := tableFixedOverlay(unsafe.Pointer(&wrong), unsafe.Sizeof(wrong))
	tableFixedRun(BFixedPlan.Entries, BFixedPlan.Count, body, dst2, &r2)
	bad := wrong.Lead == 0xAABBCCDD && wrong.X == 111 && wrong.Y == 222 && wrong.Trail == 0x11223344
	if bad {
		t.Fatalf("P4: the WRONG plan reproduced the record: %+v", wrong)
	}
}
`
	t09Run(t, schema, probe)
}
