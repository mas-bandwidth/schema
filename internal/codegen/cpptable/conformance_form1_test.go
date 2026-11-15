package cpptable

// THE C++ DRIVER'S FORM-1 FRAMING READ (docs/LAYERS/1-framing.md;
// docs/SPEC-TABLES.md §4, §6.5): the strict read is one read — the whole
// value or a refusal — and the framing battery's vectors are form-1 files,
// so a root declared `fixed table` is read there through its form-1 read
// with its class variable. THE GENERATED READER HOLDS THE ROWS: every
// generated form-1 read runs the strict wire-alone walk (TableWireStrict,
// emitted by wirestrict.go into every unit's runtime) before any value is
// decoded, so an inert array, keyed or map body (H2), bytes left inside an
// L past its count or after a record's terminator (L3, L6), a repeated field
// id (R5), an unknown field whose skipped bytes are themselves damage (R2),
// a node table riding twice (R5, T1), a node record no pointer reaches (T1)
// and an unknown node table whose record's type reference is 0 (T4) each
// refuse the whole load. The driver reads through that generated load alone
// and reports its verdict (test/conformance/cpp/main.cpp, read_answer), and
// this test holds the built driver to every framing row of the manifest:
// the verdict the row owes, the §16 text of the value a read row leaves,
// and its canonical write-back wire.
//
// It builds and runs the real driver, so it is a functional test: it skips
// by name without SCHEMA_SLOW, and fails by name under
// SCHEMA_REQUIRE_CORPUS=1 when make or the C++ compiler is missing.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppFramingStrictRows are the form-1 rows the generated strict walk alone
// refuses: the decode without it reads each of them as a value, and the
// negative control below is held to turning exactly these back into reads
// when the walk's verdict is discarded in the generated reader.
var cppFramingStrictRows = []string{
	"array_L0_inert",
	"array_L1_inert",
	"array_elements_past_count",
	"frame_repeat_last_wins",
	"frame_unknown_id_length",
	"frame_unknown_id_reference",
	"keyed_slots_past_count",
	"keyed_L1_inert",
	"map_L1_inert",
	"map_entries_past_count",
	"node_index_null",
	"node_record_bytes_after_terminator",
	"node_table_twice",
	"node_unknown_to_fixed_reader",
}

type cppFramingRow struct {
	name, unit, root, wire, verdict, value string
}

func cppFramingManifestRows(t *testing.T, root string) []cppFramingRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cppFramingRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 7 || f[0] != "framing" {
			continue
		}
		rows = append(rows, cppFramingRow{name: f[1], unit: f[2], root: f[3], wire: f[4], verdict: f[5], value: f[6]})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest carries no framing rows")
	}
	return rows
}

// cppFramingRun drives one built conformance-cpp binary over the framing
// rows alone (a derived manifest carrying nothing else, as the harness's
// own derivation writes them) and answers each row's verdict, value text
// and write-back wire as the driver wrote them.
func cppFramingRun(t *testing.T, root, bin string, rows []cppFramingRow) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	var derived strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&derived, "framing %s %s %s %s\n", r.name, r.unit, r.root, r.wire)
	}
	manifest := filepath.Join(dir, "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte(derived.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, manifest, "framing", out)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the C++ driver failed on the framing surface: %v\n%s", err, b)
	}
	answers := map[string][]byte{}
	for _, r := range rows {
		if _, err := os.Stat(filepath.Join(out, r.name+".absent")); err == nil {
			answers[r.name] = []byte("absent\n")
			continue
		}
		b, err := os.ReadFile(filepath.Join(out, r.name))
		if err != nil {
			t.Errorf("framing %s: the driver wrote no answer: %v", r.name, err)
			continue
		}
		answers[r.name] = b
		if wire, err := os.ReadFile(filepath.Join(out, r.name+".wire")); err == nil {
			answers[r.name+".wire"] = wire
		}
	}
	return answers
}

// cppFramingVerdict is the verdict half of a `framing` answer: the last
// field of its first line, `read` or `refused` (test/conformance/README.md,
// "Every read has one outcome"). The counters beside it are diagnostics
// and are never compared.
func cppFramingVerdict(answer []byte) string {
	line, _, _ := strings.Cut(string(answer), "\n")
	return strings.TrimSpace(line[strings.LastIndex(line, ",")+1:])
}

func cppFramingCheck(t *testing.T, root string, answers map[string][]byte, rows []cppFramingRow) []string {
	t.Helper()
	var wrong []string
	for _, r := range rows {
		answer, ok := answers[r.name]
		if !ok {
			continue // cppFramingRun already named the missing answer
		}
		if got := cppFramingVerdict(answer); got != r.verdict {
			wrong = append(wrong, fmt.Sprintf("%s: got %s, want %s", r.name, got, r.verdict))
			continue
		}
		if r.verdict != "read" {
			continue
		}
		// a read row owes the whole value in both forms: its §16 text
		// after the verdict line, and the canonical write-back wire
		wantText, err := os.ReadFile(filepath.Join(root, r.value))
		if err != nil {
			t.Fatal(err)
		}
		if _, text, found := strings.Cut(string(answer), "\n"); !found || text != string(wantText) {
			wrong = append(wrong, fmt.Sprintf("%s: the value text differs from %s", r.name, r.value))
			continue
		}
		wantWire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "save-wire", r.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if got := answers[r.name+".wire"]; !bytes.Equal(got, wantWire) {
			wrong = append(wrong, fmt.Sprintf("%s: the write-back wire (%d bytes) differs from save-wire/%s.bin (%d bytes)", r.name, len(got), r.name, len(wantWire)))
		}
	}
	return wrong
}

// cppConformanceMake runs one make target at the repository root, one job
// per core so the driver's table objects compile side by side, and fails
// with its output.
func cppConformanceMake(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", append([]string{"-j" + strconv.Itoa(runtime.GOMAXPROCS(0))}, args...)...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func cppConformanceRoot(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "make and the C++ compiler (it builds and runs the C++ conformance driver)")
	for _, tool := range []string{"make", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", tool, err)
			}
			t.Skipf("%s: %v", tool, err)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestCppDriverRefusesFormOneFraming holds the built C++ conformance
// driver to the whole framing battery: every damaged form-1 row refuses
// (the strict rows among them), and every readable row reads with its
// value and its write-back wire.
func TestCppDriverRefusesFormOneFraming(t *testing.T) {
	root := cppConformanceRoot(t)
	rows := cppFramingManifestRows(t, root)
	cppConformanceMake(t, root, "build/conformance-cpp")
	answers := cppFramingRun(t, root, filepath.Join(root, "build", "conformance-cpp"), rows)
	for _, wrong := range cppFramingCheck(t, root, answers, rows) {
		t.Errorf("framing %s", wrong)
	}
}

// cppStrictWalkAnchor is the generated strict walk's definition, as every
// unit's runtime header spells it once (wirestrict.go). The negative control
// below plants its fault there.
const cppStrictWalkAnchor = "inline TableRefuseReason TableWireStrict( const uint8_t * body, int64_t bytes, const TableIdTable & ids, bool & non_finite )\n{\n"

// cppStrictWalkPlant keeps the walk's own body under another name and answers
// ok in its place: the walk still runs, and its verdict is discarded, so every
// generated form-1 read decodes what the walk would have refused.
const cppStrictWalkPlant = "inline TableRefuseReason TableWireStrictSabotaged( const uint8_t * body, int64_t bytes, const TableIdTable & ids, bool & non_finite );\n" +
	"inline TableRefuseReason TableWireStrict( const uint8_t * body, int64_t bytes, const TableIdTable & ids, bool & non_finite )\n{\n" +
	"    ( void ) TableWireStrictSabotaged( body, bytes, ids, non_finite ); // SABOTAGED: the walk runs and its verdict is discarded\n" +
	"    return TableRefuseReason::ok;\n}\n" +
	"inline TableRefuseReason TableWireStrictSabotaged( const uint8_t * body, int64_t bytes, const TableIdTable & ids, bool & non_finite )\n{\n"

// TestCppDriverRefusesFormOneFramingNegativeControl plants its fault in
// the generated reader: a scratch COPY of the generated C++ tree (no tracked
// file is written to) has the strict walk's verdict discarded at its one
// definition in every runtime header that carries it, the driver is built
// against that copy with the Makefile's own flags, and the framing battery
// must go red on exactly the strict rows — each read again, as the readers
// decode them without the walk. A control that stayed green, or went red
// anywhere else, would prove the generated walk is not what refuses them.
func TestCppDriverRefusesFormOneFramingNegativeControl(t *testing.T) {
	root := cppConformanceRoot(t)
	rows := cppFramingManifestRows(t, root)
	cppConformanceMake(t, root, "build/conformance-cpp")

	scratch, err := os.MkdirTemp(filepath.Join(root, "build"), "cpp-framing-negative-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(scratch) })
	generated := filepath.Join(root, "build", "tables-generated")
	copied := filepath.Join(scratch, "generated")
	planted := 0
	err = filepath.WalkDir(generated, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(generated, path)
		if err != nil {
			return err
		}
		to := filepath.Join(copied, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if n := bytes.Count(b, []byte(cppStrictWalkAnchor)); n > 0 {
			if n != 1 {
				return fmt.Errorf("NEGATIVE CONTROL FAILED: the strict walk's definition appears %d times in %s, want 1: the plant would not be the one fault", n, rel)
			}
			b = bytes.Replace(b, []byte(cppStrictWalkAnchor), []byte(cppStrictWalkPlant), 1)
			planted++
		}
		return os.WriteFile(to, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if planted == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: no generated header defines the strict walk %q: the plant has nothing to take out", cppStrictWalkAnchor)
	}

	bin := filepath.Join(scratch, "driver-bin")
	mk := filepath.Join(scratch, "negative.mk")
	// the driver's own recipe over the planted copy: every table source is
	// its own object under make's -j, compiled from the copy with the copy's
	// headers first on the include path, then linked to the real main.cpp
	rule := fmt.Sprintf("NEGATIVE_INCLUDES = $(subst build/tables-generated/,%[1]s/,$(CONFORMANCE_INCLUDES))\n"+
		"NEGATIVE_OBJECTS = $(patsubst build/tables-generated/%%.cpp,%[2]s/objs/%%.o,$(CONFORMANCE_SOURCES))\n"+
		"%[2]s/objs/%%.o: %[1]s/%%.cpp\n\t@mkdir -p $(@D)\n\t$(CXX) $(TABLES_CXXFLAGS) $(NEGATIVE_INCLUDES) -c $< -o $@\n"+
		"%[3]s: $(NEGATIVE_OBJECTS)\n\t$(CXX) $(TABLES_CXXFLAGS) $(NEGATIVE_INCLUDES) test/conformance/cpp/main.cpp $(NEGATIVE_OBJECTS) -o %[3]s\n",
		copied, scratch, bin)
	if err := os.WriteFile(mk, []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}
	cppConformanceMake(t, root, "-f", "Makefile", "-f", mk, bin)

	answers := cppFramingRun(t, root, bin, rows)
	wrong := cppFramingCheck(t, root, answers, rows)
	got := map[string]bool{}
	for _, w := range wrong {
		name, _, _ := strings.Cut(w, ":")
		got[name] = true
	}
	for _, name := range cppFramingStrictRows {
		if !got[name] {
			t.Errorf("NEGATIVE CONTROL FAILED: with the generated strict walk's verdict discarded, framing %s did not go red: the walk is not what refuses it", name)
		}
		delete(got, name)
	}
	for name := range got {
		t.Errorf("NEGATIVE CONTROL FAILED: with the generated strict walk's verdict discarded, framing %s went red too, so the control does not localise the walk", name)
	}
}
