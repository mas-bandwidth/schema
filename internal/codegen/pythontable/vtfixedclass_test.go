package pythontable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtFixedClassSchema exercises every form-1 field kind this card carries:
// floats (plain and compressed), bits, ranged integers, enums, flags, string,
// bytes, a by-value table, and fixed and counted arrays of them, beside the
// base's bool, integer and by-value table kinds.
const vtFixedClassSchema = `package vtfc

enum Mode { Idle, Run, Jump }
flags Cap { Trace, Verbose, Extra }

table Leaf
{
    n   int32
    tag string(8)
}

table Root
{
    live         bool
    small        int8
    wide         uint64
    bits12       bits(12)
    ranged       int16 | min = -100, max = 100
    f32          float32
    f64          float64
    cf32         float32 | min = -1.0, max = 1.0, resolution = 0.25
    name         string(16)
    data         bytes(8)
    mode         Mode
    cap          Cap
    leaf         Leaf
    fixed_f32    [3]float32
    fixed_i32    [2]int32
    fixed_leaf   [2]Leaf
    fixed_mode   [2]Mode
    fixed_cap    [2]Cap
    counted_int  [..4]int32
    counted_f64  [1..3]float64
    counted_leaf [..2]Leaf
    counted_mode [..2]Mode
    counted_cap  [..3]Cap
}
`

const vtFixedClassJSON = `{
  "live": true,
  "small": -7,
  "wide": "18446744073709551615",
  "bits12": 4095,
  "ranged": 99,
  "f32": 1.5,
  "f64": -2.25,
  "cf32": 0.5,
  "name": "h\u00e9llo",
  "data": "AQID",
  "mode": "Run",
  "cap": ["Trace", "Extra"],
  "leaf": {"n": 7, "tag": "inner"},
  "fixed_f32": [1.0, 2.5, -3.25],
  "fixed_i32": [11, 12],
  "fixed_leaf": [{"n": 1, "tag": "x"}, {"n": 2, "tag": "y"}],
  "fixed_mode": ["Idle", "Jump"],
  "fixed_cap": [["Trace"], []],
  "counted_int": [-1, 0, 2147483647, 5],
  "counted_f64": [1.0, 2.0],
  "counted_leaf": [{"n": 3, "tag": "z"}],
  "counted_mode": ["Run", "Idle"],
  "counted_cap": [["Verbose"], []]
}`

// vtFixedClassEngineWire is the Go engine's canonical form-1 bytes for JSON.
func vtFixedClassEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

const vtFixedClassProbe = `import struct
from vtfc import InvalidValue, Refused, Root

with open("wire", "rb") as f:
    data = f.read()
v = Root.load(data)
saved = v.save()

def check(label, ok):
    if not ok:
        raise SystemExit("vt_fixed_class_" + label)

check("roundtrip", saved == data)
check("measure", v.measure() == len(saved))
check("values", v.mode == 2 and v.cap == 5 and v.ranged == 99 and v.bits12 == 4095)
check("text", v.name == "h\u00e9llo" and v.data == b"\x01\x02\x03")
check("floats", v.f32 == 1.5 and v.f64 == -2.25 and v.cf32 == 0.5)
check("arrays", v.fixed_f32 == [1.0, 2.5, -3.25])
check("counted", v.counted_int == [-1, 0, 2147483647, 5])
check("leaf", v.leaf.n == 7 and v.fixed_leaf[1].tag == "y" and v.counted_leaf[0].n == 3)
check("enum_array", v.fixed_mode == [1, 3] and v.counted_mode == [2, 1])
check("flags_array", v.fixed_cap == [1, 0] and v.counted_cap == [2, 0])
check("text_array", v.fixed_i32 == [11, 12] and v.counted_leaf[0].tag == "z")

def load_refusal(label, blob):
    try:
        Root.load(blob)
    except Refused as err:
        print("load", label, err.reason)
        return
    raise SystemExit("vt_fixed_class_load_" + label)

bad_form = bytearray(data)
bad_form[0] = 2
load_refusal("form2", bytes(bad_form))
load_refusal("truncated", data[:-3])
nanwire = data.replace(struct.pack("<d", -2.25), struct.pack("<Q", 0x7FF8000000000000), 1)
if nanwire == data:
    raise SystemExit("vt_fixed_class_nan_anchor")
load_refusal("nan", nanwire)

def write_refusal(label, spoil):
    value = Root.load(data)
    spoil(value)
    before = repr(value)
    try:
        value.save()
    except InvalidValue:
        check("untouched_" + label, repr(value) == before)
        print("write", label, "refused")
        return
    raise SystemExit("vt_fixed_class_write_" + label)

write_refusal("enum", lambda x: setattr(x, "mode", 99))
write_refusal("range", lambda x: setattr(x, "ranged", 101))
write_refusal("nan", lambda x: setattr(x, "f64", float("nan")))
write_refusal("f32_overflow", lambda x: setattr(x, "f32", 1e40))
write_refusal("text_bound", lambda x: setattr(x, "name", "x" * 17))
write_refusal("bytes_type", lambda x: setattr(x, "data", "not bytes"))
write_refusal("fixed_len", lambda x: setattr(x, "fixed_f32", [1.0, 2.0]))
write_refusal("counted_bound", lambda x: setattr(x, "counted_int", [1, 2, 3, 4, 5]))
write_refusal("counted_min", lambda x: setattr(x, "counted_f64", []))

print("OK")
`

func vtFixedClassFiles(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	u := loadUnit(t, vtFixedClassSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the Python table backend refuses the unit: %v", err)
	}
	return u, files
}

// TestPythonVtFixedClass is the form-1 kind card: every fixed-class field kind
// this card carries is loaded from a Go-engine wire and saved back byte for
// byte, under both pinned interpreters, and a refused read or write leaves no
// partial value and no touched caller object.
func TestPythonVtFixedClass(t *testing.T) {
	t.Parallel()
	u, files := vtFixedClassFiles(t)
	wire := vtFixedClassEngineWire(t, u, "Root", vtFixedClassJSON)
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := []byte(pyrun.ImportPrelude + vtFixedClassProbe)
	inputs := map[string][]byte{"main.py": probe, "wire": wire}
	current := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if current != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", current, floor)
	}
	for _, line := range []string{
		"load form2 message_form_as_file",
		"load truncated wire_damaged",
		"load nan non_finite_float",
		"write enum refused",
		"write range refused",
		"write nan refused",
		"write f32_overflow refused",
		"write text_bound refused",
		"write bytes_type refused",
		"write fixed_len refused",
		"write counted_bound refused",
		"write counted_min refused",
		"OK",
	} {
		if !strings.Contains(current, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, current)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "vtfc"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "vtfc/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
}

// TestPythonVtFixedClassNegativeControl moves one array body's count in a copy
// of the emitted runtime and requires the probe to go red by name.
func TestPythonVtFixedClassNegativeControl(t *testing.T) {
	t.Parallel()
	u, files := vtFixedClassFiles(t)
	wire := vtFixedClassEngineWire(t, u, "Root", vtFixedClassJSON)
	sabotaged := maps.Clone(files)
	const path = "vtfc/_schema/kind_array.py"
	text, ok := sabotaged[path]
	if !ok {
		t.Fatalf("the runtime carries no %s", path)
	}
	const anchor = "writer.varint(len(value))"
	if !strings.Contains(string(text), anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(string(text), anchor, "writer.varint(len(value) + 1)", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py": []byte(pyrun.ImportPrelude + vtFixedClassProbe),
		"wire":    wire,
	})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving an array count left the probe green")
	}
	const named = "vt_fixed_class_roundtrip"
	if !strings.Contains(stderr, named) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", named, stderr)
	}
}

// TestPythonVtFixedClassBuildVersion holds each generated corpus unit's
// BUILD_VERSION constant equal to the engine's build version (SPEC-TABLES
// §20.7). It is a Go-only gate: no interpreter runs.
func TestPythonVtFixedClassBuildVersion(t *testing.T) {
	t.Parallel()
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units, err := readManifestUnits(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	checked, refused := 0, 0
	for _, unit := range units {
		u, err := loadManifestUnit(repo, unit.paths)
		if err != nil {
			t.Fatalf("unit %s: %v", unit.key, err)
		}
		files, err := Generate(u)
		if err != nil {
			refused++
			t.Errorf("unit %s refused: %v", unit.key, err)
			continue
		}
		init := string(files[filepath.ToSlash(filepath.Join(u.Package, "__init__.py"))])
		want := fmt.Sprintf("BUILD_VERSION: Final = 0x%016x", ir.BuildVersion(u))
		if !strings.Contains(init, want) {
			t.Errorf("unit %s: the package does not carry %q", unit.key, want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("the corpus generated no unit to check")
	}
	t.Logf("%d units checked, %d refused by this surface", checked, refused)
}

type manifestUnit struct {
	key   string
	paths []string
}

// readManifestUnits reads the table manifest's `unit` lines.
func readManifestUnits(path string) ([]manifestUnit, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var units []manifestUnit
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "unit" {
			continue
		}
		units = append(units, manifestUnit{key: fields[1], paths: fields[2:]})
	}
	return units, nil
}

// loadManifestUnit parses and checks one manifest unit's schema files, a
// directory expanding to its *.schema files.
func loadManifestUnit(repo string, paths []string) (*ir.Unit, error) {
	var sources []check.SourceFile
	for _, rel := range paths {
		full := filepath.Join(repo, rel)
		info, err := os.Stat(full)
		if err != nil {
			return nil, err
		}
		var files []string
		if info.IsDir() {
			entries, err := os.ReadDir(full)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".schema") {
					files = append(files, filepath.Join(full, entry.Name()))
				}
			}
			sort.Strings(files)
		} else {
			files = []string{full}
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			name := filepath.Base(file)
			base := strings.TrimSuffix(name, ".schema")
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				return nil, fmt.Errorf("%s: %w", file, perrs[0])
			}
			sources = append(sources, check.SourceFile{Path: file, Name: name, Base: base, Bytes: data, AST: ast})
		}
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		return nil, cerrs[0]
	}
	return u, nil
}

// TestPythonVtFixedClassBuildVersionImports checks the exported version after
// importing every generated table corpus package under both interpreter pins.
func TestPythonVtFixedClassBuildVersionImports(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and strict analyzer")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units, err := readManifestUnits(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil || len(units) == 0 {
		t.Fatalf("table corpus manifest: %v (%d units)", err, len(units))
	}
	for _, unit := range units {
		t.Run(unit.key, func(t *testing.T) {
			t.Parallel()
			u, err := loadManifestUnit(repo, unit.paths)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("corpus unit refused: %v", err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Errorf("strict analyzer: %v\n%s", err, out)
			}
			probe := fmt.Sprintf("import importlib\nm = importlib.import_module(%q)\nassert m.BUILD_VERSION == %d\nprint('build-version')\n", u.Package, ir.BuildVersion(u))
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe)})
				if err != nil || strings.TrimSpace(string(out)) != "build-version" {
					t.Fatalf("imported build version: %v\n%s\n%s", err, out, stderr)
				}
			}
		})
	}
}
