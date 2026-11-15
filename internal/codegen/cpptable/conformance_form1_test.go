package cpptable

// THE C++ DRIVER'S FORM-1 FRAMING READ (docs/LAYERS/1-framing.md;
// docs/SPEC-TABLES.md §4, §6.5): the strict read is one read — the whole
// value or a refusal — and the framing battery's vectors are form-1 files,
// so a root declared `fixed table` is read there through its form-1 read
// with its class variable. The generated readers hold the framing: their own
// declaration-free strict walk (the emitted TableStrictWalk) refuses the
// battery's damaged wires before any value is decoded, and the driver reads
// through the generated load alone and reports its verdict. The rows the
// walk holds are the strict rows below: an inert array, keyed or map body
// (H2), bytes left inside an array's, keyed array's or map's L after its
// count (L6), a repeated field id (R5), an unknown field whose skipped bytes
// are themselves damage (R2), a node table riding twice (R5, T1), a record
// body with bytes after its terminator (L3) and an unknown node table in a
// reader with no reference that could reach a node (T1, T4).
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

// cppFramingStrictRows are the form-1 rows the emitted strict walk refuses
// and no other generated check does: the negative control below is held to
// turning exactly these back into reads when the walk's verdict is discarded
// from a copy of the generated code. node_index_null is not here: its null
// pointer leaves a record no pointer reaches, which the pointered read's own
// graph check refuses (§3.1, §6.5), so discarding the walk does not turn it
// back into a read.
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
	"node_record_bytes_after_terminator",
	"node_table_twice",
	"node_unknown_to_fixed_reader",
	"frame_unknown_array_count_over_cap",
	"frame_unknown_keyed_count_over_cap",
	"frame_unknown_float_non_finite",
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

// cppFramingUnknownWireRows changes an unknown field in a pinned form-1
// fixture into a count above the extent cap or a non-finite float. The
// declared reader skips that field, so only its generated strict walk can
// refuse the wire-alone condition inside it.
func cppFramingUnknownWireRows(t *testing.T, root string) []cppFramingRow {
	t.Helper()
	base, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", "frame_unknown_id_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "\x01\x10\x0d\x03\xaa\xbb\xcc"
	if !bytes.HasPrefix(base, []byte(prefix)) {
		t.Fatal("framing fixture no longer starts with the unknown field to replace")
	}
	var rows []cppFramingRow
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"frame_unknown_array_count_over_cap", []byte{14, 6, 1, 0x80, 0x80, 0x80, 0x80, 8}},
		{"frame_unknown_keyed_count_over_cap", []byte{16, 6, 1, 0x80, 0x80, 0x80, 0x80, 8}},
		{"frame_unknown_float_non_finite", []byte{10, 0, 0, 0xc0, 0x7f}},
	} {
		wire := append(append([]byte{1, 16}, tc.payload...), base[len(prefix):]...)
		path := filepath.Join(t.TempDir(), tc.name+".bin")
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, cppFramingRow{name: tc.name, unit: "tblr1", root: "Cfg", wire: path, verdict: "refused"})
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
	rows := append(cppFramingManifestRows(t, root), cppFramingUnknownWireRows(t, root)...)
	cppConformanceMake(t, root, "build/conformance-cpp")
	answers := cppFramingRun(t, root, filepath.Join(root, "build", "conformance-cpp"), rows)
	for _, wrong := range cppFramingCheck(t, root, answers, rows) {
		t.Errorf("framing %s", wrong)
	}
}

// discardStrictWalk replaces the generated strict walk's body — from its one
// definition, which `anchor` spells exactly — with a body that returns true, so
// the walk's verdict is discarded and nothing it checked refuses the read.
func discardStrictWalk(src []byte, anchor string) []byte {
	i := bytes.Index(src, []byte(anchor))
	if i < 0 {
		return src
	}
	open := bytes.IndexByte(src[i:], '{')
	if open < 0 {
		return src
	}
	open += i
	depth := 0
	end := open
	for ; end < len(src); end++ {
		switch src[end] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end++
				replacement := []byte(anchor + "\n{\n    (void) body;\n    (void) body_bytes;\n    (void) table;\n    (void) report;\n    return true; // SABOTAGED: the strict walk's verdict is discarded\n}\n")
				out := make([]byte, 0, len(src))
				out = append(out, src[:i]...)
				out = append(out, replacement...)
				out = append(out, src[end:]...)
				return out
			}
		}
	}
	return src
}

// TestCppDriverRefusesFormOneFramingNegativeControl plants its fault in the
// generated reader: the strict walk's verdict is discarded from a COPY of
// every generated Table header (no tracked file is written to), the driver is
// built against that copy with the Makefile's own flags and the driver's own
// table objects, and the framing battery must go red on exactly the strict
// rows — each read again, as the unsabotaged readers left them. A control that
// stayed green, or went red anywhere else, would prove the walk is not what
// refuses them.
func TestCppDriverRefusesFormOneFramingNegativeControl(t *testing.T) {
	root := cppConformanceRoot(t)
	rows := append(cppFramingManifestRows(t, root), cppFramingUnknownWireRows(t, root)...)
	// generate the Table headers and compile the objects the driver links
	cppConformanceMake(t, root, "conformance-cpp-objects")

	scratch, err := os.MkdirTemp(filepath.Join(root, "build"), "cpp-framing-negative-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(scratch) })

	// discard the walk's verdict from a scratch copy of every generated header:
	// a Table header's walk returns true, so no wire-alone condition refuses
	// through it and the framing battery answers what the decode alone would
	// answer. Every other header is copied verbatim, because the driver's
	// includes reach packet and wire headers beside the table ones.
	const anchor = "inline bool TableStrictWalk( const uint8_t * body, int64_t body_bytes, const TableIdTable & table, TableReport * report )"
	headers, err := filepath.Glob(filepath.Join(root, "build", "tables-generated", "*", "*.h"))
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := 0
	for _, header := range headers {
		src, err := os.ReadFile(header)
		if err != nil {
			t.Fatal(err)
		}
		planted := src
		if strings.HasSuffix(header, "Table.h") {
			if n := bytes.Count(src, []byte(anchor)); n != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the generated strict walk %q appears %d times in %s, want 1: the plant below has nothing to take out", anchor, n, header)
			}
			planted = discardStrictWalk(src, anchor)
			sabotaged++
		}
		rel, err := filepath.Rel(filepath.Join(root, "build", "tables-generated"), header)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(scratch, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, planted, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if sabotaged == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: no generated Table header carried the strict walk")
	}

	bin := filepath.Join(scratch, "driver-bin")
	mk := filepath.Join(scratch, "negative.mk")
	// the planted headers shadow the generated ones for the driver's own main.cpp
	// alone; the table objects the driver links are the unsabotaged ones, so the
	// control recompiles one translation unit, not every table source over again
	rule := fmt.Sprintf("NEGATIVE_SCRATCH := %s\n%s: conformance-cpp-objects\n\t$(CXX) $(TABLES_CXXFLAGS) $(subst -Ibuild/tables-generated,-I$(NEGATIVE_SCRATCH),$(CONFORMANCE_INCLUDES)) %s $(CONFORMANCE_OBJECTS) -o %s\n", scratch, bin, filepath.Join(root, "test", "conformance", "cpp", "main.cpp"), bin)
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
