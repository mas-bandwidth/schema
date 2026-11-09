package cpptable

// THE C++ DRIVER'S FORM-1 FRAMING READ (docs/LAYERS/1-framing.md;
// docs/SPEC-TABLES.md §4, §6.5): the strict read is one read — the whole
// value or a refusal — and the framing battery's vectors are form-1 files,
// so a root declared `fixed table` is read there through its form-1 read
// with its class variable. Ten of the battery's rows are damaged wires the
// generated C++ readers decoded as values: an inert array, keyed or map
// body (H2), a repeated field id (R5), an unknown field whose skipped bytes
// are themselves damage (R2), a node table riding twice (R5, T1), a node
// record no pointer reaches (T1) and an unknown node table whose record's
// type reference is 0 (T4). The driver now walks the whole wire by the
// wire-alone rules before it decodes (test/conformance/cpp/main.cpp,
// framing_strict_ok), and this test holds the built driver to every
// framing row of the manifest: the verdict the row owes, the §16 text of
// the value a read row leaves, and its canonical write-back wire.
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
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppFramingStrictRows are the ten form-1 rows that must refuse and did
// not: the negative control below is held to turning exactly these back
// into reads when the strict walk is taken out of the driver.
var cppFramingStrictRows = []string{
	"array_L0_inert",
	"array_L1_inert",
	"frame_repeat_last_wins",
	"frame_unknown_id_length",
	"frame_unknown_id_reference",
	"keyed_L1_inert",
	"map_L1_inert",
	"node_index_null",
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
	for _, line := range strings.Split(string(data), "\n") {
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
	fields := strings.Split(line, ",")
	return strings.TrimSpace(fields[len(fields)-1])
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

// cppConformanceMake runs one make target at the repository root and
// fails with its output.
func cppConformanceMake(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", args...)
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
// (the ten strict rows among them), and every readable row reads with its
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

// TestCppDriverRefusesFormOneFramingNegativeControl plants its fault in
// the real artifact: the strict form-1 walk is taken out of a COPY of the
// driver (no tracked file is written to), the copy is built with the
// Makefile's own flags beside the generated tables, and the framing
// battery must go red on exactly the ten strict rows — each read again,
// as the unsabotaged readers left them. A control that stayed green, or
// went red anywhere else, would prove the walk is not what refuses them.
func TestCppDriverRefusesFormOneFramingNegativeControl(t *testing.T) {
	root := cppConformanceRoot(t)
	rows := cppFramingManifestRows(t, root)
	src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "cpp", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "framing_strict_ok( wire.data(), wire.size() )"
	if n := bytes.Count(src, []byte(anchor)); n != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the strict walk's call %q appears %d times in test/conformance/cpp/main.cpp, want 1: the plant below has nothing to take out", anchor, n)
	}
	planted := bytes.Replace(src, []byte(anchor), []byte("( framing_strict_ok( wire.data(), wire.size() ), true ) /* SABOTAGED: the walk runs and its verdict is discarded */"), 1)

	scratch, err := os.MkdirTemp(filepath.Join(root, "build"), "cpp-framing-negative-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(scratch) })
	plantedSrc := filepath.Join(scratch, "main.cpp")
	if err := os.WriteFile(plantedSrc, planted, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(scratch, "driver-bin")
	mk := filepath.Join(scratch, "negative.mk")
	rule := fmt.Sprintf("%s: build/tables-generated/.stamp\n\t$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) %s $(CONFORMANCE_SOURCES) -o %s\n", bin, plantedSrc, bin)
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
			t.Errorf("NEGATIVE CONTROL FAILED: with the strict walk taken out, framing %s did not go red: the walk is not what refuses it", name)
		}
		delete(got, name)
	}
	for name := range got {
		t.Errorf("NEGATIVE CONTROL FAILED: with the strict walk taken out, framing %s went red too, so the control does not localise the walk", name)
	}
}
