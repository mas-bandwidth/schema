package jstable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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

// A kind mismatch refuses the whole load (docs/SPEC-TABLES.md §6.5). The
// wire-fuzz pipe has to name that refusal: a null value with the refused bit
// clear is the oracle refusing and the leg staying silent.
func TestWireFuzzKindMismatchNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "test/tables/V1.schema")
	wire, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/v2_seams.bin"))
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	var got tabletext.Report
	ok, derr := tablewire.Decode(model, model.New(model.Lookup("Cfg")), wire, &got)
	if ok && derr == nil && !got.Refused {
		t.Fatalf("oracle accepted the kind-mismatch wire: report=%+v", got)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	src, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "v1", home+"Variable.js"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"tblv1", "Cfg"} {
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
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		if reply[0] != 0 || reply[22] != 1 {
			t.Fatalf("retaining=%d kind mismatch was not named refused: loaded=%d malformed=%d refused=%d reply=%x", retaining, reply[0], reply[21], reply[22], reply[:47])
		}
		if binary.LittleEndian.Uint32(reply[5:9]) == 0 {
			t.Fatalf("retaining=%d kind mismatch counter stayed 0", retaining)
		}
	}
}
func TestWireFuzzArrayTailNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "testdata/conformance/feature-witnesses/full/Features.schema")
	clean, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/feature-witnesses/full/ArrayRoot.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// Mutant 416 of feature_array_root at seed 24845619678: three i8 elements
	// that widen into []int32, then bytes left inside that array's L. The
	// oracle damages the tail; the leg must name it and must not accept.
	tail := []byte{
		0x01, 0x01, 0x0e, 0x0e, 0x02, 0x03, 0x03, 0x00, 0x00, 0x00, 0x11, 0x00, 0x00, 0x00,
		0xfb, 0xff, 0xff, 0xff, 0x00, 0xfb, 0x67, 0x4d, 0x1a, 0xcf, 0x7b, 0x27, 0x21, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	model := tabletext.NewModel(u)
	root := model.Lookup("ArrayRoot")
	if root == nil {
		t.Fatal("ArrayRoot is not in the feature-witnesses unit")
	}
	var cleanReport tabletext.Report
	ok, derr := tablewire.Decode(model, model.New(root), clean, &cleanReport)
	if !ok || derr != nil || cleanReport.Refused {
		t.Fatalf("oracle refused the clean ArrayRoot wire: ok=%t err=%v report=%+v", ok, derr, cleanReport)
	}
	var tailReport tabletext.Report
	ok, derr = tablewire.Decode(model, model.New(root), tail, &tailReport)
	if ok && derr == nil && !tailReport.Refused {
		t.Fatalf("oracle accepted the array-tail wire: report=%+v", tailReport)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	src, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", home+"Variable.js"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, wire := range []struct {
		name    string
		bytes   []byte
		loaded  byte
		damaged byte
	}{
		{"clean", clean, 1, 0},
		{"tail", tail, 0, 1},
	} {
		for _, retaining := range []byte{0, 1} {
			var input bytes.Buffer
			binary.Write(&input, binary.LittleEndian, uint32(1))
			for _, name := range []string{"featurewitnesses", "ArrayRoot"} {
				binary.Write(&input, binary.LittleEndian, uint16(len(name)))
				input.WriteString(name)
			}
			input.WriteByte(1)
			input.WriteByte(retaining)
			binary.Write(&input, binary.LittleEndian, uint32(0))
			binary.Write(&input, binary.LittleEndian, uint32(len(wire.bytes)))
			input.Write(wire.bytes)
			cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
			cmd.Stdin = &input
			var output, stderr bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s retaining=%d driver: %v\n%s", wire.name, retaining, err, stderr.String())
			}
			data := output.Bytes()
			if len(data) < 48 || data[0] != 1 {
				t.Fatalf("%s retaining=%d protocol len %d known %d stderr %s", wire.name, retaining, len(data), data[0], stderr.String())
			}
			reply := data[1:]
			if reply[0] != wire.loaded || reply[21] != wire.damaged {
				t.Fatalf("%s retaining=%d loaded=%d malformed=%d refused=%d, want loaded=%d malformed=%d reply=%x",
					wire.name, retaining, reply[0], reply[21], reply[22], wire.loaded, wire.damaged, reply[:47])
			}
			if wire.loaded == 1 && reply[22] != 0 {
				t.Fatalf("%s retaining=%d clean wire was refused", wire.name, retaining)
			}
		}
	}
}

// Mutant 462 of feature_array_root at seed 24845619678: the array count is
// 2^31, past the extent cap. The oracle refuses. The pipe names that refusal
// and does not call the unread tail damage.
func TestWireFuzzCountCapNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "testdata/conformance/feature-witnesses/full/Features.schema")
	wire := []byte{
		0x01, 0x01, 0x0e, 0x12, 0x04, 0x80, 0x80, 0x80, 0x80, 0x08, 0x03, 0x00, 0x00, 0x00,
		0x11, 0x00, 0x00, 0x00, 0xfb, 0xff, 0xff, 0xff, 0x00, 0xfb, 0x67, 0x4d, 0x1a, 0xcf,
		0x7b, 0x27, 0x21, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	model := tabletext.NewModel(u)
	root := model.Lookup("ArrayRoot")
	if root == nil {
		t.Fatal("ArrayRoot is not in the feature-witnesses unit")
	}
	var got tabletext.Report
	ok, derr := tablewire.Decode(model, model.New(root), wire, &got)
	if ok && derr == nil && !got.Refused {
		t.Fatalf("oracle accepted the count-cap wire: report=%+v", got)
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"featurewitnesses", "ArrayRoot"} {
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
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		if reply[0] != 0 || reply[21] != 0 || reply[22] != 1 {
			t.Fatalf("retaining=%d count past the extent cap was not named refused: loaded=%d malformed=%d refused=%d reply=%x",
				retaining, reply[0], reply[21], reply[22], reply[:47])
		}
	}
}

// Mutant 4898 of graph_tree at seed 24845619678: the record-type pass writes
// the last id-table slot into one node record. That slot is a field id, so
// the record is unknown. Its body still holds the pointer that reaches a
// placed child. The oracle accepts with one unknown. The leg must accept too,
// and the saved bytes must be the oracle's canonical encoding.
func TestWireFuzzUnknownNodeEdgesKeepTheChildJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 28 || seed[27] != 0x07 {
		t.Fatalf("graph_tree record-type byte moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[27] = 0x0f
	model := tabletext.NewModel(u)
	root := model.Lookup("Scene")
	if root == nil {
		t.Fatal("Scene is not in the graphdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pointers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pointers", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, wire := range []struct {
		name    string
		bytes   []byte
		unknown uint32
	}{
		{"seed", seed, 0},
		{"mutant", mutant, 1},
	} {
		inst := model.New(root)
		var rep tabletext.Report
		ok, derr := tablewire.Decode(model, inst, wire.bytes, &rep)
		if !ok || derr != nil || rep.Refused || rep.Unknown != int(wire.unknown) {
			t.Fatalf("%s oracle: ok=%t err=%v refused=%t unknown=%d report=%+v", wire.name, ok, derr, rep.Refused, rep.Unknown, rep)
		}
		enc, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s oracle encode: %v", wire.name, err)
		}
		for _, retaining := range []byte{0, 1} {
			var input bytes.Buffer
			binary.Write(&input, binary.LittleEndian, uint32(1))
			for _, name := range []string{"graphdemo", "Scene"} {
				binary.Write(&input, binary.LittleEndian, uint16(len(name)))
				input.WriteString(name)
			}
			input.WriteByte(1)
			input.WriteByte(retaining)
			binary.Write(&input, binary.LittleEndian, uint32(0))
			binary.Write(&input, binary.LittleEndian, uint32(len(wire.bytes)))
			input.Write(wire.bytes)
			cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
			cmd.Stdin = &input
			var output, stderr bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s retaining=%d driver: %v\n%s", wire.name, retaining, err, stderr.String())
			}
			data := output.Bytes()
			if len(data) < 48 || data[0] != 1 {
				t.Fatalf("%s retaining=%d protocol len %d known %d stderr %s", wire.name, retaining, len(data), data[0], stderr.String())
			}
			reply := data[1:]
			gotUnknown := binary.LittleEndian.Uint32(reply[1:5])
			savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
			saved := data[48:]
			if reply[0] != 1 || reply[21] != 0 || reply[22] != 0 || gotUnknown != wire.unknown || savedLen != int64(len(enc)) || !bytes.Equal(saved, enc) {
				t.Fatalf("%s retaining=%d loaded=%d malformed=%d refused=%d unknown=%d saved=%d want unknown=%d saved=%d",
					wire.name, retaining, reply[0], reply[21], reply[22], gotUnknown, savedLen, wire.unknown, len(enc))
			}
		}
	}
}

// Mutant 5135 of graph_tree at seed 24845619678: a node record's first field
// reference is zero, so the record's frame holds bytes after its terminator.
// The oracle refuses that wire. The leg must refuse it as damage.
func TestWireFuzzRecordBodyTailNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 46 || seed[45] != 0x08 {
		t.Fatalf("graph_tree field reference moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[45] = 0x00
	model := tabletext.NewModel(u)
	root := model.Lookup("Scene")
	if root == nil {
		t.Fatal("Scene is not in the graphdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pointers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pointers", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, wire := range []struct {
		name     string
		bytes    []byte
		loaded   byte
		damaged  byte
		accepted bool
	}{
		{"seed", seed, 1, 0, true},
		{"tail", mutant, 0, 1, false},
	} {
		inst := model.New(root)
		var rep tabletext.Report
		ok, derr := tablewire.Decode(model, inst, wire.bytes, &rep)
		accepted := ok && derr == nil && !rep.Refused
		if accepted != wire.accepted {
			t.Fatalf("%s oracle accepted=%t ok=%t err=%v refused=%t report=%+v", wire.name, accepted, ok, derr, rep.Refused, rep)
		}
		for _, retaining := range []byte{0, 1} {
			var input bytes.Buffer
			binary.Write(&input, binary.LittleEndian, uint32(1))
			for _, name := range []string{"graphdemo", "Scene"} {
				binary.Write(&input, binary.LittleEndian, uint16(len(name)))
				input.WriteString(name)
			}
			input.WriteByte(1)
			input.WriteByte(retaining)
			binary.Write(&input, binary.LittleEndian, uint32(0))
			binary.Write(&input, binary.LittleEndian, uint32(len(wire.bytes)))
			input.Write(wire.bytes)
			cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
			cmd.Stdin = &input
			var output, stderr bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s retaining=%d driver: %v\n%s", wire.name, retaining, err, stderr.String())
			}
			data := output.Bytes()
			if len(data) < 48 || data[0] != 1 {
				t.Fatalf("%s retaining=%d protocol len %d known %d stderr %s", wire.name, retaining, len(data), data[0], stderr.String())
			}
			reply := data[1:]
			if reply[0] != wire.loaded || reply[21] != wire.damaged || (wire.loaded == 1 && reply[22] != 0) {
				t.Fatalf("%s retaining=%d loaded=%d malformed=%d refused=%d, want loaded=%d malformed=%d",
					wire.name, retaining, reply[0], reply[21], reply[22], wire.loaded, wire.damaged)
			}
		}
	}
}

// Mutant 50426 of stream_chain at seed 24845619678: Feed's frame field
// reference is the last id-table slot, an id this root does not declare.
// The payload is still the union, and its arm names a node. The oracle
// accepts with one unknown. The leg must accept too, and the saved bytes
// must be the oracle's canonical encoding. A node-record body whose
// terminator leaves bytes in its frame stays damage (the graph_tree tail).
func TestWireFuzzUnknownUnionNamesItsEdgesJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/stream/Stream.schema")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/stream_chain.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 8 || seed[7] != 0x02 {
		t.Fatalf("stream_chain field reference moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[7] = 0x07
	model := tabletext.NewModel(u)
	root := model.Lookup("Feed")
	if root == nil {
		t.Fatal("Feed is not in the streamdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "stream"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stream", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(model, inst, mutant, &rep)
	if !ok || derr != nil || rep.Refused || rep.Unknown != 1 {
		t.Fatalf("oracle: ok=%t err=%v refused=%t unknown=%d report=%+v", ok, derr, rep.Refused, rep.Unknown, rep)
	}
	enc, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"streamdemo", "Feed"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(mutant)))
		input.Write(mutant)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		gotUnknown := binary.LittleEndian.Uint32(reply[1:5])
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		saved := data[48:]
		if reply[0] != 1 || reply[21] != 0 || reply[22] != 0 || gotUnknown != 1 || savedLen != int64(len(enc)) || !bytes.Equal(saved, enc) {
			t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d unknown=%d saved=%d want saved=%d",
				retaining, reply[0], reply[21], reply[22], gotUnknown, savedLen, len(enc))
		}
	}
}

// Mutant 50473 of stream_chain at seed 24845619678: Feed's frame arm
// reference is the last id-table slot, an arm this union does not declare.
// The payload is still the table arm, and it names a node. The oracle
// accepts with one unknown. The leg must accept too, and the saved bytes
// must be the oracle's canonical encoding.
func TestWireFuzzUnknownArmNamesItsEdgesJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/stream/Stream.schema")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/stream_chain.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 10 || seed[9] != 0x03 {
		t.Fatalf("stream_chain arm reference moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[9] = 0x07
	model := tabletext.NewModel(u)
	root := model.Lookup("Feed")
	if root == nil {
		t.Fatal("Feed is not in the streamdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "stream"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stream", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(model, inst, mutant, &rep)
	if !ok || derr != nil || rep.Refused || rep.Unknown != 1 {
		t.Fatalf("oracle: ok=%t err=%v refused=%t unknown=%d report=%+v", ok, derr, rep.Refused, rep.Unknown, rep)
	}
	enc, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"streamdemo", "Feed"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(mutant)))
		input.Write(mutant)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		gotUnknown := binary.LittleEndian.Uint32(reply[1:5])
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		saved := data[48:]
		if reply[0] != 1 || reply[21] != 0 || reply[22] != 0 || gotUnknown != 1 || savedLen != int64(len(enc)) || !bytes.Equal(saved, enc) {
			t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d unknown=%d saved=%d want saved=%d",
				retaining, reply[0], reply[21], reply[22], gotUnknown, savedLen, len(enc))
		}
	}
}

// Mutant 57948 of blob_shared at seed 24845619678: Catalog's thumb field
// reference is the last id-table slot, the bytes-blob type id, which this
// root does not declare as a field. The payload is still a node index, and
// it names the same blob record alias names. The oracle refuses the wire
// blob_shared. The leg must refuse it too, and must not call the wire damage.
func TestWireFuzzUnknownBlobEdgeNamesTheShareJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/blobs/Assets.schema")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 9 || seed[8] != 0x02 {
		t.Fatalf("blob_shared field reference moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[8] = 0x05
	model := tabletext.NewModel(u)
	root := model.Lookup("Catalog")
	if root == nil {
		t.Fatal("Catalog is not in the blobdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blobs", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(model, inst, mutant, &rep)
	if ok || derr == nil || tablewire.RefusalReason(derr) != tablewire.ReasonBlobShared || !rep.Refused {
		t.Fatalf("oracle: ok=%t err=%v refused=%t report=%+v", ok, derr, rep.Refused, rep)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"blobdemo", "Catalog"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(mutant)))
		input.Write(mutant)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		gotUnknown := binary.LittleEndian.Uint32(reply[1:5])
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		if reply[0] != 0 || reply[21] != 0 || reply[22] == 0 || gotUnknown != 1 || savedLen != -1 {
			t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d unknown=%d saved=%d",
				retaining, reply[0], reply[21], reply[22], gotUnknown, savedLen)
		}
	}
}

// Mutant 59636 of blob_shared_string at seed 24845619678: Docs' pages field
// reference is the last id-table slot, the string-blob type id, which this
// root does not declare as a field. The payload is still the map, and both
// entries name one string-blob record. The oracle refuses the wire
// blob_shared. The leg must refuse it too, and must not call the wire damage.
func TestWireFuzzUnknownMapBlobEdgeNamesTheShareJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/maps")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/blob_shared_string.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 2 || seed[1] != 0x01 {
		t.Fatalf("blob_shared_string field reference moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[1] = 0x05
	model := tabletext.NewModel(u)
	root := model.Lookup("Docs")
	if root == nil {
		t.Fatal("Docs is not in the mapdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "maps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "maps", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(model, inst, mutant, &rep)
	if ok || derr == nil || tablewire.RefusalReason(derr) != tablewire.ReasonBlobShared || !rep.Refused {
		t.Fatalf("oracle: ok=%t err=%v refused=%t report=%+v", ok, derr, rep.Refused, rep)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"mapdemo", "Docs"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(mutant)))
		input.Write(mutant)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		gotUnknown := binary.LittleEndian.Uint32(reply[1:5])
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		if reply[0] != 0 || reply[21] != 0 || reply[22] == 0 || gotUnknown != 1 || savedLen != -1 {
			t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d unknown=%d saved=%d",
				retaining, reply[0], reply[21], reply[22], gotUnknown, savedLen)
		}
	}
}

// Mutant 80924 of list_of_maps at seed 24845619678: the map-key id in the
// id table has its low bit flipped, so each Squad entry's key is absent and
// both entries of a map take the key's default. The oracle refuses the wire
// wire_damaged, because map keys must ascend strictly. The leg must name
// that damage and must not accept the wire.
func TestWireFuzzMapKeyOrderNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/lists")
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/list_of_maps.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 116 || seed[115] != 0xec {
		t.Fatalf("list_of_maps key id moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[115] = 0xed
	model := tabletext.NewModel(u)
	root := model.Lookup("Army")
	if root == nil {
		t.Fatal("Army is not in the listdemo unit")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u)
	js, exists := files[home+"Variable.js"]
	if !exists {
		t.Fatalf("no %sVariable.js", home)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lists"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lists", home+"Variable.js"), js, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(model, inst, mutant, &rep)
	if ok || derr == nil || tablewire.RefusalReason(derr) != tablewire.ReasonWireDamaged || !rep.Refused {
		t.Fatalf("oracle: ok=%t err=%v refused=%t report=%+v", ok, derr, rep.Refused, rep)
	}
	for _, retaining := range []byte{0, 1} {
		var input bytes.Buffer
		binary.Write(&input, binary.LittleEndian, uint32(1))
		for _, name := range []string{"listdemo", "Army"} {
			binary.Write(&input, binary.LittleEndian, uint16(len(name)))
			input.WriteString(name)
		}
		input.WriteByte(1)
		input.WriteByte(retaining)
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(mutant)))
		input.Write(mutant)
		cmd := exec.Command(node, "test/conformance/js/main.mjs", "wire-fuzz")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+dir)
		cmd.Stdin = &input
		var output, stderr bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
		}
		data := output.Bytes()
		if len(data) < 48 || data[0] != 1 {
			t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
		}
		reply := data[1:]
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		if reply[0] != 0 || reply[21] != 1 || reply[22] != 0 || savedLen != -1 {
			t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d saved=%d",
				retaining, reply[0], reply[21], reply[22], savedLen)
		}
	}
}

func TestWireFuzzFixedRootNamesPreviousFormJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(repo, "test/tables/P3.schema"))
	seed, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/chain_value.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) < 2 || seed[0] != 0x01 || seed[1] != 0x01 {
		t.Fatalf("chain_value moved: len %d", len(seed))
	}
	mutant := bytes.Clone(seed)
	mutant[1] = 0x04
	model := tabletext.NewModel(u)
	root := model.Lookup("Chain")
	if root == nil || !root.FixedDeclared {
		t.Fatal("Chain is not a fixed root of tblp3")
	}
	for _, wire := range [][]byte{seed, mutant} {
		inst := model.New(root)
		var rep tabletext.Report
		ok, derr := tablewire.Decode(model, inst, wire, &rep)
		if ok || derr == nil || tablewire.RefusalReason(derr) != tablewire.FixedReasonPreviousForm || !rep.Refused {
			t.Fatalf("oracle: ok=%t err=%v refused=%t report=%+v", ok, derr, rep.Refused, rep)
		}
	}
	variable, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := generateFixedFiles(u, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "p3")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(sub, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range variable {
		write(name, data)
	}
	for name, data := range fixed {
		write(name, data)
	}
	for name, data := range generateViewFile(u) {
		write(name, data)
	}
	for _, wire := range [][]byte{seed, mutant} {
		for _, retaining := range []byte{0, 1} {
			var input bytes.Buffer
			binary.Write(&input, binary.LittleEndian, uint32(1))
			for _, name := range []string{"tblp3", "Chain"} {
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
				t.Fatalf("retaining=%d driver: %v\n%s", retaining, err, stderr.String())
			}
			data := output.Bytes()
			if len(data) < 48 || data[0] != 1 {
				t.Fatalf("retaining=%d protocol len %d known %d stderr %s", retaining, len(data), data[0], stderr.String())
			}
			reply := data[1:]
			savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
			if reply[0] != 0 || reply[21] != 0 || reply[22] != 1 || savedLen != -1 {
				t.Fatalf("retaining=%d loaded=%d malformed=%d refused=%d saved=%d",
					retaining, reply[0], reply[21], reply[22], savedLen)
			}
		}
	}
}

func TestWireFuzzEmptyMessageNamesTheRefusalJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(repo, "tables/backend/Backend.schema"))
	model := tabletext.NewModel(u)
	root := model.Lookup("LoginRequest")
	if root == nil {
		t.Fatal("LoginRequest is not a root of backenddemo")
	}
	var rep tabletext.Report
	n, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{model.New(root)}, nil, nil, &rep)
	if n != 0 || ok || derr == nil || tablewire.RefusalReason(derr) != tablewire.ReasonWireDamaged || !rep.Refused {
		t.Fatalf("oracle empty: n=%d ok=%t err=%v refused=%t", n, ok, derr, rep.Refused)
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "backend")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range generatedTree(u) {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			continue
		}
		if err := os.WriteFile(filepath.Join(sub, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	full, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/login_full_message.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(full) < 2 || full[0] != 0x02 {
		t.Fatalf("login_full_message moved: len %d", len(full))
	}
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatal(err)
	}
	fullInst := model.New(root)
	var fullRep tabletext.Report
	fn, fok, ferr := tablewire.DecodeMessages(model, []*tabletext.Instance{fullInst}, full, &vocabulary, &fullRep)
	if fn != 1 || !fok || ferr != nil || fullRep.Refused {
		t.Fatalf("oracle full: n=%d ok=%t err=%v refused=%t", fn, fok, ferr, fullRep.Refused)
	}
	encoded, err := tablewire.EncodeMessages(model, []*tabletext.Instance{fullInst})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 3 {
		t.Fatalf("oracle full save len %d", len(encoded))
	}
	// An empty batch and a form the message reader does not own are refusals
	// on every message root. A delivered login batch saves EncodeMessages,
	// including that encoding's length and byte 2.
	cases := []struct {
		root    string
		wire    []byte
		loaded  byte
		damaged byte
		refused byte
		saved   int64
		want    []byte
	}{
		{"LoginRequest", nil, 0, 1, 0, -1, nil},
		{"MatchResult", nil, 0, 1, 0, -1, nil},
		{"StorePurchase", nil, 0, 1, 0, -1, nil},
		{"LoginRequest", []byte{0x01}, 0, 0, 1, -1, nil},
		{"LoginRequest", full, 1, 0, 0, int64(len(encoded)), encoded},
	}
	for _, tc := range cases {
		out := driveWireFuzzMessage(t, node, repo, dir, "backenddemo", tc.root, tc.wire)
		if len(out) < 48 || out[0] != 1 {
			t.Fatalf("%s len %d: protocol", tc.root, len(out))
		}
		reply := out[1:]
		savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
		if reply[0] != tc.loaded || reply[21] != tc.damaged || reply[22] != tc.refused || savedLen != tc.saved {
			t.Fatalf("%s wire %d: loaded=%d malformed=%d refused=%d saved=%d",
				tc.root, len(tc.wire), reply[0], reply[21], reply[22], savedLen)
		}
		if tc.want == nil {
			continue
		}
		saved := reply[47:]
		if int64(len(saved)) < savedLen || !bytes.Equal(saved[:savedLen], tc.want) || saved[2] != tc.want[2] {
			at := 0
			for at < len(saved) && at < len(tc.want) && saved[at] == tc.want[at] {
				at++
			}
			t.Fatalf("%s save len %d oracle %d first difference at byte %d", tc.root, savedLen, len(tc.want), at)
		}
	}
}

func driveWireFuzzMessage(t *testing.T, node, repo, dir, unit, root string, wire []byte) []byte {
	t.Helper()
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(1))
	for _, name := range []string{unit, root} {
		binary.Write(&input, binary.LittleEndian, uint16(len(name)))
		input.WriteString(name)
	}
	input.WriteByte(2)
	input.WriteByte(0)
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
		t.Fatalf("%s driver: %v\n%s", root, err, stderr.String())
	}
	return output.Bytes()
}

func TestWireFuzzMessageSaveMatchesTheOracleJavaScript(t *testing.T) {
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(repo, "tables/backend/Backend.schema"))
	model := tabletext.NewModel(u)
	root := model.Lookup("LoginRequest")
	if root == nil {
		t.Fatal("LoginRequest is not a root of backenddemo")
	}
	mutant, err := hex.DecodeString("02004200000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800403801")
	if err != nil {
		t.Fatal(err)
	}
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatal(err)
	}
	inst := model.New(root)
	var rep tabletext.Report
	n, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{inst}, mutant, &vocabulary, &rep)
	if n != 1 || !ok || derr != nil || rep.Unknown != 2 || rep.Refused {
		t.Fatalf("oracle read: n=%d ok=%t err=%v unknown=%d refused=%t", n, ok, derr, rep.Unknown, rep.Refused)
	}
	encoded, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 50 || encoded[2] != 0x22 {
		t.Fatalf("oracle save len %d byte2 %02x", len(encoded), encoded[2])
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "backend")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range generatedTree(u) {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			continue
		}
		if err := os.WriteFile(filepath.Join(sub, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := driveWireFuzzMessage(t, node, repo, dir, "backenddemo", "LoginRequest", mutant)
	if len(out) < 48 || out[0] != 1 {
		t.Fatalf("protocol len %d", len(out))
	}
	reply := out[1:]
	savedLen := int64(binary.LittleEndian.Uint64(reply[39:47]))
	if reply[0] != 1 || reply[21] != 0 || reply[22] != 0 || savedLen != int64(len(encoded)) {
		t.Fatalf("loaded=%d malformed=%d refused=%d saved=%d oracle=%d", reply[0], reply[21], reply[22], savedLen, len(encoded))
	}
	saved := reply[47:]
	if int64(len(saved)) < savedLen || !bytes.Equal(saved[:savedLen], encoded) {
		at := 0
		for at < len(saved) && at < len(encoded) && saved[at] == encoded[at] {
			at++
		}
		t.Fatalf("save len %d oracle %d first difference at byte %d", savedLen, len(encoded), at)
	}
}
