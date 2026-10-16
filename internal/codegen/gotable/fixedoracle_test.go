package gotable

// THE GO LEG'S SHARED BYTE ORACLE AND HOSTILE-INPUT GATE, OVER EVERY FIXED
// VECTOR (ROADMAP.md "Shared byte oracle and round-trip conformance" and
// "Hostile-input checks and negative controls", the go column). Law:
// docs/FIXED-FORM-ALGORITHM.md §7 rows 1 and 3 — "Read EACH of the nine and
// save it back; the bytes must be identical", and each negative control "under
// ITS OWN NAME, nothing decoded, no counter moved, and every byte of the
// caller's destination as it was before the load" — and §5.3's "TOTAL OVER THE
// BATCH, TOO". TestFixedRoadmapGoT12ConformanceHostile holds fx1.bin alone;
// this gate holds all nine of the reference's corpus files, and cuts its
// hostile images from those same reference bytes rather than from a file the
// leg's own writer produced.
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// build/fixedform-corpus, read through fixedCorpus (a SKIP on a bare tree, a
// FAILURE under SCHEMA_REQUIRE_CORPUS=1). The record count of each file is read
// from manifest.txt, never from the bytes under test (§5.9 #32). Each vector
// compiles and runs the leg's own emitted unit, packet types included, through fixedOracleRun (SCHEMA_SLOW=1).

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedOracleVectors is §7 row 1's nine: the corpus file, the schema that wrote
// it, and its root.
var fixedOracleVectors = []struct {
	file, schema, root string
}{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

func TestGoFixedSharedByteOracleAndHostileInput(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedOracleManifestRecords(t, filepath.Join(corpus, "manifest.txt"))
	for _, v := range fixedOracleVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			n, ok := records[v.file]
			if !ok {
				t.Fatalf("the manifest carries no line for %s", v.file)
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "..", v.schema))
			if err != nil {
				t.Fatal(err)
			}
			schema := string(data)
			pkg := ""
			for _, line := range strings.Split(schema, "\n") {
				if rest, ok := strings.CutPrefix(line, "package "); ok {
					pkg = strings.TrimSpace(rest)
					break
				}
			}
			if pkg == "" {
				t.Fatalf("%s names no package", v.schema)
			}
			probe := strings.NewReplacer(
				"PKG", pkg,
				"ROOT", v.root,
				"FILE", strconv.Quote(filepath.Join(corpus, v.file)),
				"RECORDS", strconv.Itoa(n),
			).Replace(fixedOracleProbe)
			fixedOracleRun(t, schema, probe)
		})
	}
}

// fixedOracleRun compiles the unit's packet types and its fixed tables into one
// package beside the probe and runs it. A fixture whose packet types import the
// sibling serialize.go runtime gets it through a replace of ../serialize.go, as
// t09Run does for a 128-bit store.
func fixedOracleRun(t *testing.T, schema, probe string) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	u := unitFrom(t, schema)
	files := map[string][]byte{}
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	mod := "module probe\n\ngo 1.26\n"
	for _, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			runtime, err := filepath.Abs("../../../../serialize.go")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(runtime, "go.mod")); err != nil {
				t.Fatalf("the packet types import the sibling serialize.go runtime, not staged at %s: %v", runtime, err)
			}
			mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			break
		}
	}
	files["go.mod"] = []byte(mod)
	files["values_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated fixed-table runtime: %v\n%s", err, out)
	}
}

// fixedOracleManifestRecords reads `records=` per `file=` line of the corpus
// manifest, in decimal.
func fixedOracleManifestRecords(t *testing.T, path string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		file, count := "", -1
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				file = v
			case "records":
				if n, err := strconv.Atoi(v); err == nil {
					count = n
				}
			}
		}
		if file != "" && count >= 0 {
			out[file] = count
		}
	}
	return out
}

// fixedOracleProbe runs inside the emitted package. PKG, ROOT, FILE and RECORDS
// are substituted per vector.
var fixedOracleProbe = `package PKG

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"unsafe"
)

const poison = 0x5A

func poisoned(n int) ([]ROOT, []byte) {
	v := make([]ROOT, n+1)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), int(unsafe.Sizeof(v[0]))*(n+1))
	for i := range b {
		b[i] = poison
	}
	return v[:n], b
}

func untouched(b []byte) bool {
	for _, c := range b {
		if c != poison {
			return false
		}
	}
	return true
}

func dirty() TableReport {
	return TableReport{Widened: 1, Unknown: 2, KindMismatch: 3, Clamped: 4, Duplicate: 5, Malformed: true, Retained: 6, RetainLost: 7}
}

func TestOracleAndHostile(t *testing.T) {
	file, err := os.ReadFile(FILE)
	if err != nil {
		t.Fatal(err)
	}
	const records = RECORDS
	if records < 1 {
		t.Fatalf("the manifest names %d records", records)
	}

	// §7 row 1, the shared byte oracle: the header's hash and layout are the
	// emitted ones, the file reads clean, and it saves back byte for byte.
	if h := binary.LittleEndian.Uint64(file[TableFixedHashAt:]); h != uint64(ROOTFixedHash) {
		t.Fatalf("oracle: the header hash 0x%016x is not the emitted 0x%016x", h, uint64(ROOTFixedHash))
	}
	lb := int(binary.LittleEndian.Uint32(file[TableFixedHeaderBytes:]))
	if lb != len(ROOTFixedLayout) || !bytes.Equal(file[TableFixedHeaderBytes+4:TableFixedHeaderBytes+4+lb], ROOTFixedLayout) {
		t.Fatal("oracle: the layout block is not the emitted layout")
	}
	plan := make([]TableFixedEntry, 4096)
	back := make([]ROOT, records)
	r := dirty()
	if n := ROOTFixedLoad(back, file, plan, &r); n != records || r != (TableReport{}) {
		t.Fatalf("oracle: the reference file did not read clean: n=%d %+v", n, r)
	}
	out := make([]byte, ROOTFixedMeasure(records))
	if m := ROOTFixedSave(back, out); m != int64(len(file)) || !bytes.Equal(out, file) {
		t.Fatalf("oracle: write-read-write differs (%d bytes against the file's %d)", m, len(file))
	}

	// §7 row 3, the negative controls, cut from the reference bytes: each is
	// one break in a file that reads clean, refused under its own name with
	// the whole report otherwise zero, and every byte of the destination —
	// one slot past the batch included — still the poison (§5.3, TOTAL OVER
	// THE BATCH, TOO).
	layoutAt := TableFixedHeaderBytes + 4
	recordsAt := layoutAt + lb
	recordAt := func(k int) int { return recordsAt + k*int(ROOTFixedRecordBytes) }
	hash := uint64(ROOTFixedHash)
	type control struct {
		name     string
		forge    func(b []byte) []byte
		capacity int
		planCap  int
		want     TableReport
	}
	refused := func(why string) TableReport { return TableReport{Verdict: TableOpenRefused, Reason: why} }
	controls := []control{
		{name: "previous_form", forge: func(b []byte) []byte { b[0] = 1; return b }, want: refused("previous_form")},
		{name: "message_form_as_file", forge: func(b []byte) []byte { b[0] = 2; return b }, want: refused("message_form_as_file")},
		{name: "newer_form", forge: func(b []byte) []byte { b[0] = 6; return b }, want: refused("newer_form")},
		{name: "reserved byte", forge: func(b []byte) []byte { b[4] = 1; return b }, want: TableReport{Malformed: true, Reason: "reserved_not_zero"}},
		{name: "layout_newer", forge: func(b []byte) []byte {
			binary.LittleEndian.PutUint64(b[TableFixedHashAt:], hash^1)
			return b
		}, want: TableReport{Verdict: TableOpenRefused, Reason: "layout_newer", LayoutHash: hash ^ 1}},
		{name: "layout length past the file", forge: func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[TableFixedHeaderBytes:], uint32(len(b)))
			return b
		}, want: refused("layout_malformed")},
		{name: "known hash over a different layout", forge: func(b []byte) []byte {
			b[recordsAt-1] ^= 0x01
			return b
		}, want: refused("layout_malformed")},
		{name: "plan_too_large", planCap: 1, want: refused("plan_too_large")},
		{name: "batch_too_large", capacity: records - 1, want: refused("batch_too_large")},
		{name: "no_layout in the first record", forge: func(b []byte) []byte {
			b[recordAt(0)] ^= 0x01
			return b
		}, want: refused("no_layout")},
		{name: "no_layout in the last record", forge: func(b []byte) []byte {
			b[recordAt(records-1)] ^= 0x01
			return b
		}, want: refused("no_layout")},
		{name: "ragged tail", forge: func(b []byte) []byte { return append(b, 0) }, want: TableReport{Malformed: true, Reason: "truncated"}},
	}
	if ROOTFixedPlan.Count <= 1 {
		t.Fatalf("plan_too_large: the identity plan has %d entries, so a one-entry slice fits it", ROOTFixedPlan.Count)
	}
	for _, c := range controls {
		forged := append([]byte(nil), file...)
		if c.forge != nil {
			forged = c.forge(forged)
		}
		capacity := records
		if c.name == "batch_too_large" {
			capacity = c.capacity
		}
		p := plan
		if c.planCap > 0 {
			p = plan[:c.planCap]
		}
		dst, raw := poisoned(capacity)
		r := dirty()
		n := ROOTFixedLoad(dst, forged, p, &r)
		if n != -1 || r != c.want {
			t.Errorf("%s: n=%d %+v, want -1 %+v", c.name, n, r, c.want)
		}
		if !untouched(raw) {
			t.Errorf("%s: the refused image wrote the caller's destination", c.name)
		}
	}
}
`
