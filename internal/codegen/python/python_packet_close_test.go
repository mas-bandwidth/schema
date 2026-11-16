package python

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

func TestPythonPacketClose(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	known := map[string]bool{"bench_table.bin": true}
	for _, pin := range packetPins() {
		dir := ""
		if strings.Contains(strings.Join(pin.paths, "|"), "packet-defaults") {
			dir = "packet-defaults/"
		}
		key := dir + pin.name + ".bin"
		if known[key] && pin.name != "bench_table" {
			t.Errorf("duplicate packet pin %s", key)
		}
		known[key] = true
	}
	for _, dir := range []string{"", "packet-defaults"} {
		matches, err := filepath.Glob(filepath.Join(root, "testdata", "wire", dir, "*.bin"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range matches {
			key := filepath.ToSlash(filepath.Join(dir, filepath.Base(path)))
			if !known[key] {
				t.Errorf("uncovered packet pin %s", key)
			}
		}
	}
	for _, pin := range packetPins() {
		if pin.name == "bench_table" {
			continue
		}
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			if pin.note != "" {
				packetCloseMulti(t, root, pin.name)
				return
			}
			paths := make([]string, len(pin.paths))
			for i, p := range pin.paths {
				paths[i] = filepath.Join(root, filepath.FromSlash(p))
			}
			u := loadPythonUnit(t, paths...)
			if blocker := rootBlocker(u, pin.root); blocker != "" {
				t.Fatalf("packet golden is blocked by %s", blocker)
			}
			pruned := rootClosure(u, pin.root)
			files, err := Generate(pruned)
			if err != nil {
				t.Fatal(err)
			}
			intField, boolField, intMin, intMax := topLevelFields(pruned, pin.root)
			probe := packetProbe(pruned, pin.root, pruned.DeclFile[pin.root], intField, boolField, intMin, intMax)
			pinDir := filepath.Join(root, "testdata", "wire")
			if strings.Contains(strings.Join(pin.paths, "|"), "packet-defaults") {
				pinDir = filepath.Join(pinDir, "packet-defaults")
			}
			data, err := os.ReadFile(filepath.Join(pinDir, pin.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range []struct {
				name string
				fn   func(testing.TB, string, string, map[string][]byte) []byte
			}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
				t.Run(run.name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					writeTree(t, dir, files)
					got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "input.bin": data})))
					if got != "ok" {
						t.Fatalf("%s: %s", pin.name, got)
					}
				})
			}
		})
	}
}

func packetCloseMulti(t *testing.T, root, name string) {
	t.Helper()
	u := loadPythonUnit(t, filepath.Join(root, "examples"))
	roots := []string{}
	var sequence string
	switch name {
	case "degenerate":
		roots = strings.Fields("Vec2 SpanF64 SpanU64 SpanI64 SpanOne SpanChunk SpanTail SpanTwice Trio TrioSole TrioFirst TrioStraddle")
		sequence = `
stream = ReadStream(data)
values = []
for kind in [Vec2, SpanF64, SpanU64, SpanI64, SpanOne, SpanChunk, SpanTail, SpanTwice, Trio, TrioSole, TrioFirst, TrioStraddle]:
    v = kind()
    if not v._s_read(stream): raise RuntimeError("read " + kind.__name__ + ": " + str(stream.refusal()))
    values.append(v)
if not stream.serialize_end(): raise RuntimeError("end " + str(stream.refusal()))
w = WriteStream((len(data) + 7) // 8 * 8)
for v in values:
    v._s_validate(type(v).__name__)
    v._s_write(w)
w.flush()
if w.data() != data: raise RuntimeError("write-back differs")
`
	case "clauses":
		roots = strings.Fields("W13 W17 W26 W1 W52 W50 F13 ArrTri3 ArrEleven HoldsEmptyUnion Strs ArrNested Sole")
		sequence = `
rows = []
def add(kind, bits): rows.append((kind, (bits + 7) // 8))
for c in [0,1,3,4,5,7,12]: add(W13, 4 + 13*c)
for c in [0,1,2,3,4,9]: add(W17, 4 + 17*c)
for c in [0,1,2,3,6]: add(W26, 3 + 26*c)
for c in [0,1,3,4,5,20]: add(W1, 5 + c)
for c in range(4): add(W52, 2 + 52*c)
for c in range(4): add(W50, 2 + 50*c)
add(F13, 91)
for c in [0,1,3,4,5,10]: add(ArrTri3, 4 + 3*c)
add(ArrEleven, 99)
for _ in range(3): add(HoldsEmptyUnion, 14)
for bits in [27,155,75]: add(Strs, bits)
for c in range(5): add(ArrNested, 11 + 11*c)
add(Sole, 13)
`
	case "joins":
		roots = strings.Fields("ArmsAgree ArmsDisagree ArmEmpty ArmAlign ArmsNested ArmArray HoldsUneven ArrUneven RegainAfterAlign")
		sequence = `
rows = []
def add(kind, bits): rows.append((kind, (bits + 7) // 8))
for f in range(2):
    add(ArmsAgree, 24)
    add(ArmsDisagree, 24 if f else 16)
    add(ArmEmpty, 32 if f else 16)
    add(ArmAlign, 56 if f else 24)
    add(ArmAlign, 24)
for o in range(2):
    for i in range(2): add(ArmsNested, (40 if i else 16) if o else 23)
for f in range(2):
    for c in range(4): add(ArmArray, 15 + 13*c if f else 22)
for bits in [18,21,55]: add(HoldsUneven, bits)
for n in [0,5,44,49]: add(ArrUneven, 10+n)
for c in range(4):
    for sl in [0,4]: add(RegainAfterAlign, ((5+2+13*c+3+7)//8*8)+8*sl+84)
`
	default:
		t.Fatalf("unknown concatenated golden %s", name)
	}
	pruned := rootClosure(u, roots...)
	files, err := Generate(pruned)
	if err != nil {
		t.Fatal(err)
	}
	var imports strings.Builder
	for _, n := range roots {
		fmt.Fprintf(&imports, "from %s.%s import %s\n", u.Package, pruned.DeclFile[n], n)
	}
	probe := pyrun.ImportPrelude + imports.String() + fmt.Sprintf("from %s._schema.serialize import ReadStream, WriteStream\n", u.Package) + `
data = open("input.bin", "rb").read()
` + sequence
	if name != "degenerate" {
		probe += `
off = 0
for kind, size in rows:
    segment = data[off:off+size]
    if len(segment) != size: raise RuntimeError("short segment " + kind.__name__)
    value = kind.read(segment)
    if value.write() != segment: raise RuntimeError("write-back " + kind.__name__)
    off += size
if off != len(data): raise RuntimeError("trailing bytes " + str(len(data)-off))
`
	}
	probe += "\nprint('ok')\n"
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "input.bin": data})))
			if got != "ok" {
				t.Fatal(got)
			}
		})
	}
}

func TestPythonPacketCloseAnalyzer(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"examples", "examples128", "bench/corpus/Bench.schema", "test/packet-defaults/Defaults.schema"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			u := loadPythonUnit(t, filepath.Join(repoRoot(t), filepath.FromSlash(path)))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, err := pyrun.Analyze(t, dir, u.Package)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("mypy: %v\n%s", err, out)
			}
		})
	}
}

func TestPythonPacketCloseNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadPythonUnit(t, filepath.Join(root, "examples"))
	pruned := rootClosure(u, "ShipCreate")
	files, err := Generate(pruned)
	if err != nil {
		t.Fatal(err)
	}
	base := pruned.DeclFile["ShipCreate"]
	module := pruned.Package + "/" + base + ".py"
	original := string(files[module])
	marker := "self.health = _s_ri.value"
	if strings.Count(original, marker) != 1 {
		t.Fatal("generated reader health marker missing or ambiguous")
	}
	files[module] = []byte(strings.Replace(original, marker, "self.health = 1 + _s_ri.value", 1))
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	intField, boolField, intMin, intMax := topLevelFields(pruned, "ShipCreate")
	probe := packetProbe(pruned, "ShipCreate", base, intField, boolField, intMin, intMax)
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, _, err := pyrun.Try(t, pin.exe(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "input.bin": data})
			if err == nil || !strings.Contains(out, "FAILED: write-back differs") {
				t.Fatalf("control did not fail on write-back: %v\n%s", err, out)
			}
		})
	}
}

func TestPythonPacketCloseAnalyzerNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadPythonUnit(t, filepath.Join(repoRoot(t), "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := u.Package + "/" + u.DeclFile["ShipCreate"] + ".py"
	files[name] = append(files[name], []byte("\n_s_analyzer_control: int = \"fault\"\n")...)
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, u.Package)
	if err == nil || !strings.Contains(out, "Incompatible types in assignment") || !strings.Contains(out, filepath.Base(name)) {
		t.Fatalf("analyzer did not name planted fault: %v\n%s", err, out)
	}
}

func TestPythonPacketDefaults(t *testing.T) {
	t.Parallel()
	packetDefaultsProbe(t, false)
}

func TestPythonPacketDefaultsNegativeControl(t *testing.T) {
	t.Parallel()
	packetDefaultsProbe(t, true)
}

func packetDefaultsProbe(t *testing.T, fault bool) {
	t.Helper()
	root := repoRoot(t)
	u := loadPythonUnit(t, filepath.Join(root, "test", "packet-defaults", "Defaults.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, corpus := range []string{"examples", "examples128"} {
		additional, err := Generate(loadPythonUnit(t, filepath.Join(root, corpus)))
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range additional {
			files[name] = data
		}
	}
	if fault {
		name := u.Package + "/Defaults.py"
		code := string(files[name])
		marker := `self.token = b"\\n\\t"`
		if strings.Count(code, marker) != 1 {
			t.Fatalf("default token marker missing: %s", marker)
		}
		files[name] = []byte(strings.Replace(code, marker, `self.token = b"wrong"`, 1))
	}
	driver, err := os.ReadFile(filepath.Join(root, "test", "packet-defaults", "python", "main.py"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"main.py": driver}
	for _, name := range []string{"sample-defaults", "sample-empty", "sample-short", "batch-defaults", "zero-count", "conditional-on", "conditional-off", "choice-sample"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "packet-defaults", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name+".bin"] = data
	}
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, stderr, err := pyrun.Try(t, pin.exe(t), dir, "main.py", inputs)
			if fault {
				if err == nil || !strings.Contains(stderr, "constructor text bytes") {
					t.Fatalf("planted default did not fail by name: %v\n%s\n%s", err, out, stderr)
				}
			} else if err != nil || strings.TrimSpace(out) != "ok" {
				t.Fatalf("default probe: %v\n%s\n%s", err, out, stderr)
			}
		})
	}
}
