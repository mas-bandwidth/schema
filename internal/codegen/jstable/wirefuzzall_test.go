package jstable

import (
	"bytes"
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWireFuzzAllTargetJavaScript pins the JavaScript wire-fuzz target to the
// harness default roster.  The target must not quietly fall back to the old
// Card-only manifest or become a prerequisite nobody runs from test-js.
func TestWireFuzzAllTargetJavaScript(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../../make/js.mk")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "tables-js-wire-fuzz-all:")
	if start < 0 {
		t.Fatal("make/js.mk declares no tables-js-wire-fuzz-all target")
	}
	end := strings.Index(s[start:], "\n\n")
	if end < 0 {
		end = len(s) - start
	}
	target := s[start : start+end]
	if !strings.Contains(target, "./build/conformance-harness wire-fuzz") ||
		!strings.Contains(target, "--driver 'test/conformance/js/driver wire-fuzz'") {
		t.Fatalf("tables-js-wire-fuzz-all does not invoke the JavaScript wire fuzzer:\n%s", target)
	}
	if strings.Contains(target, "--manifest") {
		t.Fatalf("tables-js-wire-fuzz-all must use the harness default roster, not a manifest:\n%s", target)
	}
	for _, want := range []string{"0 divergences", "seeds over", "mutants"} {
		if !strings.Contains(target, want) {
			t.Errorf("tables-js-wire-fuzz-all lacks the %q coverage check", want)
		}
	}
	if !strings.Contains(s, "test-js: tables-js-wire-fuzz-all") {
		t.Fatal("test-js does not run tables-js-wire-fuzz-all")
	}
	// The absent set is pinned at 0 (every file-form and message-form seed
	// in the default roster is bound), so a roster root dropping to absent
	// fails the target. tblg1 Guarded's four answer: the target carries an
	// optional over a variable closure (docs/SPEC-TABLES.md §2.3).
	if !strings.Contains(s, "TABLES_JS_WIRE_FUZZ_ABSENT := 0\n") {
		t.Error("make/js.mk does not pin the absent set at 0 seeds")
	}
	if !strings.Contains(target, "! grep -qE '[0-9]+ seeds absent '") {
		t.Errorf("tables-js-wire-fuzz-all does not check the pinned absent count:\n%s", target)
	}
	// The four form-1 roster roots no other unit carries are generated for
	// the leg and bound by the driver's fuzzUnits, or the harness counts them
	// absent and the default roster is not the whole roster.
	drv, err := os.ReadFile("../../../test/conformance/js/main.mjs")
	if err != nil {
		t.Fatal(err)
	}
	d := string(drv)
	if !strings.Contains(d, "textUnits[unit] ?? fuzzUnits[unit]") {
		t.Error("main.mjs wireFuzzCodec does not fall back to fuzzUnits")
	}
	for _, u := range []struct{ dir, schema, unit, file string }{
		{"a2", "A2", "tbla2", "a2/Tbla2Variable.js"},
		{"k1", "K1", "tblk1", "k1/Tblk1Variable.js"},
		{"k2", "K2", "tblk2", "k2/Tblk2Variable.js"},
		{"p2", "P2", "tblp2", "p2/Tblp2Variable.js"},
	} {
		gen := "--out build/tables-generated-js/" + u.dir + " test/tables/" + u.schema + ".schema"
		if !strings.Contains(s, gen) {
			t.Errorf("make/js.mk does not generate %s for the JavaScript leg (%q)", u.schema, gen)
		}
		if !strings.Contains(d, u.unit+": \""+u.file+"\"") {
			t.Errorf("main.mjs fuzzUnits does not bind %s to %s", u.unit, u.file)
		}
	}
	// tblg1 is generated and bound through wireRows, which wireFuzzCodec
	// reads first: the target carries ?T over a variable closure
	// (docs/SPEC-TABLES.md §2.3), so its four roster seeds answer.
	if !strings.Contains(s, "--out build/tables-generated-js/g1 test/tables/G1.schema") {
		t.Error("make/js.mk does not generate G1 for the JavaScript leg")
	}
	if !strings.Contains(d, `["tblg1", "Guarded", "g1/Tblg1Variable.js"]`) {
		t.Error("main.mjs wireRows does not bind tblg1.Guarded")
	}
}

// TestWireFuzzCodecFailsByNameJavaScript runs the driver's wire-fuzz arm over a
// one-root roster (the harness's framing: count, then per root a u16 unit, a
// u16 root, the form byte and the retain byte) against a generated tree the
// test writes, and pins docs/SPEC-TABLES.md §4.2's named failure: a unit the
// driver names a module for (tbla2, fuzzUnits) whose module is missing, or
// lacks the root's Load, LoadMeasure or Save, fails naming unit.root, while a
// unit it names no module for answers known=0 (absent, which make/js.mk pins).
// It is a toolchain test: node comes through jsNode(t).
func TestWireFuzzCodecFailsByNameJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	roster := func(unit, root string) []byte {
		var b bytes.Buffer
		binary.Write(&b, binary.LittleEndian, uint32(1))
		binary.Write(&b, binary.LittleEndian, uint16(len(unit)))
		b.WriteString(unit)
		binary.Write(&b, binary.LittleEndian, uint16(len(root)))
		b.WriteString(root)
		b.WriteByte(1) // form 1, the file form
		b.WriteByte(0) // no retention
		return b.Bytes()
	}
	run := func(gen, unit, root string) (string, string, error) {
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+gen)
		cmd.Stdin = bytes.NewReader(roster(unit, root))
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		return out.String(), errb.String(), err
	}

	// a named module that is missing
	missing := t.TempDir()
	if _, stderr, err := run(missing, "tbla2", "Root"); err == nil || !strings.Contains(stderr, "tbla2.Root: the wire-fuzz module is missing") {
		t.Errorf("missing module: want a failure naming tbla2.Root, got err=%v stderr=%q", err, stderr)
	}

	// a named module without the root's LoadMeasure
	partial := t.TempDir()
	if err := os.MkdirAll(filepath.Join(partial, "a2"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "export function RootLoad() { return null; }\nexport function RootSave() { return null; }\n"
	if err := os.WriteFile(filepath.Join(partial, "a2", "Tbla2Variable.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(partial, "tbla2", "Root"); err == nil || !strings.Contains(stderr, "tbla2.Root: the variable wire surface RootLoadMeasure is missing") {
		t.Errorf("partial module: want a failure naming tbla2.Root and RootLoadMeasure, got err=%v stderr=%q", err, stderr)
	}

	// a unit the driver names no module for answers absent, not a failure
	stdout, stderr, err := run(missing, "nosuchunit", "Root")
	if err != nil || stdout != "\x00" {
		t.Errorf("unnamed unit: want known=0 and a clean exit, got err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

// The managed Load API has builder count-cap semantics. The protocol's
// Refused bit names a wire-form refusal, not this out-of-band storage limit.
func TestWireFuzzBuilderCountReportJavaScript(t *testing.T) {
	t.Skip("layer 4 (js): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/lists")
	wire, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/fuzz-vectors/list_count_cross_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, model.New(model.Lookup("Album")), wire, &report)
	if ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("builder oracle: ok=%v err=%v report=%+v", ok, err, report)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lists"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, "lists", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"listdemo", "Album"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(wire)))
		input.Write(wire)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("driver: %v\n%s", err, stderr.String())
		}
		data := output.Bytes()
		if len(data) != 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len%d %x", retaining, len(data), data)
		}
		reply := data[1:]
		if reply[0] != 0 || reply[21] != 0 || reply[22] != 0 || int64(binary.LittleEndian.Uint64(reply[39:47])) != -1 {
			t.Fatalf("retaining=%d count refusal became a wire report event: %x", retaining, reply)
		}
		for _, off := range []int{1, 5, 9, 13, 17, 31, 35} {
			if binary.LittleEndian.Uint32(reply[off:off+4]) != 0 {
				t.Fatalf("unexpected report event at%d", off)
			}
		}
	}
}

// A failed independent measurement must not suppress the managed reader report.
func TestWireFuzzMalformedMeasureReportJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/lists")
	wire := []byte{}
	model := tabletext.NewModel(u)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, model.New(model.Lookup("Album")), wire, &report)
	if ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("builder oracle: ok=%v err=%v report=%+v", ok, err, report)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lists"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, "lists", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"listdemo", "Album"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(wire)))
		input.Write(wire)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("driver: %v\n%s", err, stderr.String())
		}
		data := output.Bytes()
		if len(data) != 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len%d %x", retaining, len(data), data)
		}
		reply := data[1:]
		if reply[0] != 0 || reply[21] != 1 || reply[22] != 0 || int64(binary.LittleEndian.Uint64(reply[39:47])) != -1 {
			t.Fatalf("retaining=%d empty malformed load did not preserve its report: %x", retaining, reply)
		}
		for _, off := range []int{1, 5, 9, 13, 17, 31, 35} {
			if binary.LittleEndian.Uint32(reply[off:off+4]) != 0 {
				t.Fatalf("unexpected report event at%d", off)
			}
		}
	}
}
