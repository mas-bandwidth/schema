package python

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadPythonUnit builds one compilation unit from schema paths, a directory
// contributing its *.schema files.
func loadPythonUnit(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			tree, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			sources = append(sources, check.SourceFile{
				Path: name, Name: filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: tree,
			})
		}
	}
	u, errs := check.Unit(sources)
	if len(errs) > 0 {
		t.Fatalf("check %v: %v", paths, errs[0])
	}
	return u
}

// rootClosure is the declarations a root type needs by value, in their own
// files. It makes a one-root unit out of a corpus file the codec cannot carry
// whole, so a carried root's C++ pin can still be read.
func rootClosure(u *ir.Unit, root string) *ir.Unit {
	byName := map[string]ir.Decl{}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			byName[declName(d)] = d
		}
	}
	keep := map[string]bool{}
	var add func(string)
	add = func(name string) {
		if name == "" || keep[name] {
			return
		}
		d, ok := byName[name]
		if !ok {
			return
		}
		keep[name] = true
		switch d := d.(type) {
		case *ir.Struct:
			for _, f := range d.Fields {
				if f.Type.Kind == ir.TNamed {
					add(f.Type.Name)
				}
			}
		case *ir.Union:
			for _, v := range d.Variants {
				add(v.Type)
			}
		}
	}
	add(root)
	out := &ir.Unit{
		Package: u.Package, ProtocolId: u.ProtocolId,
		DeclFile: map[string]string{}, Consts: map[string]*ir.Const{},
		Enums: map[string]*ir.Enum{}, Flags: map[string]*ir.Flags{},
		Structs: map[string]*ir.Struct{}, Unions: map[string]*ir.Union{},
	}
	for _, f := range u.Files {
		var decls []ir.Decl
		for _, d := range f.Decls {
			if !keep[declName(d)] {
				continue
			}
			decls = append(decls, d)
			name := declName(d)
			out.DeclFile[name] = f.Base
			switch d := d.(type) {
			case *ir.Const:
				out.Consts[d.Name] = d
			case *ir.Enum:
				out.Enums[d.Name] = d
			case *ir.Flags:
				out.Flags[d.Name] = d
			case *ir.Struct:
				out.Structs[d.Name] = d
			case *ir.Union:
				out.Unions[d.Name] = d
			}
		}
		if len(decls) > 0 {
			out.Files = append(out.Files, &ir.File{Base: f.Base, Path: f.Path, Decls: decls})
		}
	}
	return out
}

func declName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Const:
		return d.Name
	case *ir.Enum:
		return d.Name
	case *ir.Flags:
		return d.Name
	case *ir.Struct:
		return d.Name
	case *ir.Union:
		return d.Name
	}
	return ""
}

func rootBlocker(u *ir.Unit, root string) string {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if declName(d) == root {
				return declBlocker(u, d, map[string]bool{})
			}
		}
	}
	return "missing root"
}

func declBlocker(u *ir.Unit, d ir.Decl, seen map[string]bool) string {
	switch d := d.(type) {
	case *ir.Struct:
		if seen[d.Name] {
			return ""
		}
		seen[d.Name] = true
		return itemsBlocker(u, d.Items, seen)
	case *ir.Union:
		if seen[d.Name] {
			return ""
		}
		seen[d.Name] = true
		for _, v := range d.Variants {
			if v.Void() {
				continue
			}
			if k := fieldBlocker(u, v.F, seen); k != "" {
				return k
			}
		}
	}
	return ""
}

func itemsBlocker(u *ir.Unit, items []ir.Item, seen map[string]bool) string {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if k := fieldBlocker(u, item.F, seen); k != "" {
				return k
			}
		case *ir.Branch:
			if k := itemsBlocker(u, item.Then, seen); k != "" {
				return k
			}
			if k := itemsBlocker(u, item.Else, seen); k != "" {
				return k
			}
		case *ir.ConstItem:
			return "const"
		case *ir.ReservedItem:
			return "reserved"
		case *ir.AlignItem:
			return "align"
		}
	}
	return ""
}

func fieldBlocker(u *ir.Unit, f *ir.Field, seen map[string]bool) string {
	if fieldKind(f) == "" {
		return blockingKind(f)
	}
	if f.Type.Kind != ir.TNamed {
		return ""
	}
	var target ir.Decl
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		target = ref
	case *ir.Union:
		target = ref
	default:
		return ""
	}
	if target == nil {
		return ""
	}
	return declBlocker(u, target, seen)
}

type packetPin struct {
	name  string
	paths []string
	root  string
	note  string
}

// packetPins maps every C++-pinned packet golden under testdata/wire to the
// one root type it pins. The concatenated goldens and the table-wire pin have
// no single root and carry their note instead.
func packetPins() []packetPin {
	examples := []string{"examples"}
	ludicrous := []string{"examples128"}
	bench := []string{"bench/corpus/Bench.schema"}
	real := []string{"bench/corpus/RealWorld.schema"}
	defaults := []string{"test/packet-defaults/Defaults.schema"}
	return []packetPin{
		{"shipcreate_flags", examples, "ShipCreate", ""},
		{"chat", examples, "Chat", ""},
		{"compressed_probe", examples, "CompressedProbe", ""},
		{"inputpacket", examples, "InputPacket", ""},
		{"probearray", examples, "ProbeArray", ""},
		{"probebits", examples, "ProbeBits", ""},
		{"probecollider", examples, "ProbeCollider", ""},
		{"probe_header", examples, "ProbeHeader", ""},
		{"testdata", examples, "TestData", ""},
		{"rigidbody_at_rest", examples, "RigidBody", ""},
		{"rigidbody_moving", examples, "RigidBody", ""},
		{"clauses", examples, "", "concatenated multi-root pin"},
		{"joins", examples, "", "concatenated multi-root pin"},
		{"degenerate", examples, "", "concatenated multi-root pin"},
		{"ludicrous_state", ludicrous, "LudicrousState", ""},
		{"ludicrous_state_untargeted", ludicrous, "LudicrousState", ""},
		{"degenerate_probe", ludicrous, "DegenerateProbe", ""},
		{"unsigned_probe", ludicrous, "UnsignedProbe", ""},
		{"bench_bits", bench, "BenchBits", ""},
		{"bench_ints", bench, "BenchInts", ""},
		{"bench_mixed", bench, "BenchMixed", ""},
		{"bench_packet", bench, "BenchPacket", ""},
		{"bench_table", nil, "", "table-wire pin"},
		{"real_packet", real, "RealPacket", ""},
		{"sample-defaults", defaults, "Sample", ""},
		{"sample-empty", defaults, "Sample", ""},
		{"sample-short", defaults, "Sample", ""},
		{"conditional-on", defaults, "Conditional", ""},
		{"conditional-off", defaults, "Conditional", ""},
		{"batch-defaults", defaults, "Batch", ""},
		{"zero-count", defaults, "ZeroCount", ""},
		{"choice-sample", defaults, "Choice", ""},
	}
}

// packetProbe is the Python side of the gate: read the pin, write it back
// byte-identical, refuse a trailing byte and a short read, and refuse the
// wrong Python type and an out-of-range integer on write.
func packetProbe(u *ir.Unit, root, base, intField, boolField string, intMin, intMax *big.Int) string {
	intChecks := ""
	if intField != "" {
		above := new(big.Int).Add(intMax, big.NewInt(1))
		below := new(big.Int).Sub(intMin, big.NewInt(1))
		intChecks += fmt.Sprintf(`
for bad in (%s, %s):
    v = %s()
    v.%s = bad
    try:
        v.write()
    except InvalidValue:
        pass
    else:
        print("FAILED: an out-of-range int was written")
        sys.exit(1)
v = %s()
v.%s = True
try:
    v.write()
except InvalidValue:
    pass
else:
    print("FAILED: a bool where an int is declared was written")
    sys.exit(1)
`, above.String(), below.String(), root, intField, root, intField)
	}
	boolChecks := ""
	if boolField != "" {
		boolChecks = fmt.Sprintf(`
v = %s()
v.%s = 1
try:
    v.write()
except InvalidValue:
    pass
else:
    print("FAILED: an int where a bool is declared was written")
    sys.exit(1)
`, root, boolField)
	}
	return pyrun.ImportPrelude + fmt.Sprintf(`
import sys
from %s.%s import %s
from %s import InvalidValue, Refused

data = open("input.bin", "rb").read()
try:
    value = %s.read(data)
except Refused as exc:
    print("FAILED: the pin was refused: " + exc.reason)
    sys.exit(1)
try:
    again = value.write()
except InvalidValue as exc:
    print("FAILED: the read value was refused on write: " + str(exc))
    sys.exit(1)
if again != data:
    print("FAILED: write-back differs")
    print("got  " + again.hex())
    print("want " + data.hex())
    sys.exit(1)
try:
    %s.read(data + b"\x00")
except Refused:
    pass
else:
    print("FAILED: a trailing byte was accepted")
    sys.exit(1)
if len(data) > 0:
    try:
        %s.read(data[:-1])
    except Refused:
        pass
    else:
        print("FAILED: a short read was accepted")
        sys.exit(1)
fresh = %s()
if repr(fresh) == "":
    print("FAILED: empty repr")
    sys.exit(1)
if not (fresh == %s()):
    print("FAILED: a fresh value does not equal its own constructor")
    sys.exit(1)
if fresh != %s():
    print("FAILED: equality is not by value")
    sys.exit(1)
%s
%s
print("ok")
`, u.Package, base, root, u.Package, root, root, root, root, root, root, intChecks, boolChecks)
}

func topLevelFields(u *ir.Unit, root string) (intField, boolField string, intMin, intMax *big.Int) {
	st, ok := u.Structs[root]
	if !ok {
		return "", "", nil, nil
	}
	for _, f := range st.Fields {
		switch {
		case intField == "" && f.Type.Kind == ir.TInt:
			intField = f.Name
			intMin, intMax = intBounds(f)
		case boolField == "" && f.Type.Kind == ir.TBool:
			boolField = f.Name
		}
	}
	return intField, boolField, intMin, intMax
}

// TestPythonPacketCore reads every single-root C++-pinned packet golden whose
// kinds the core carries, requires its value written back byte-identical under
// both pinned interpreters, refuses hostile input and wrong-typed values, runs
// the analyzer over each generated tree, and lists every pin it skips with the
// kind that blocks it (docs/PORT-PYTHON.md, THE HARNESS).
func TestPythonPacketCore(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	units := map[string]*ir.Unit{}
	load := func(paths []string) *ir.Unit {
		key := strings.Join(paths, "|")
		if u, ok := units[key]; ok {
			return u
		}
		abs := make([]string, len(paths))
		for i, p := range paths {
			abs[i] = filepath.Join(root, filepath.FromSlash(p))
		}
		u := loadPythonUnit(t, abs...)
		units[key] = u
		return u
	}

	var carried []packetPin
	skipped := map[string]string{}
	for _, pin := range packetPins() {
		if pin.note != "" {
			skipped[pin.name] = pin.note
			continue
		}
		u := load(pin.paths)
		blocker := rootBlocker(u, pin.root)
		if blocker != "" {
			skipped[pin.name] = blocker
			continue
		}
		carried = append(carried, pin)
	}
	// every pin the wire has is accounted for
	entries, err := filepath.Glob(filepath.Join(root, "testdata", "wire", "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, pin := range packetPins() {
		known[pin.name+".bin"] = true
	}
	for _, p := range entries {
		if !known[filepath.Base(p)] {
			t.Errorf("testdata/wire/%s has no packet root in the harness", filepath.Base(p))
		}
	}
	for _, pin := range carried {
		if note, ok := skipped[pin.name]; ok {
			t.Errorf("%s is carried but is on the skip list (%s)", pin.name, note)
		}
	}
	if len(carried) == 0 {
		t.Fatal("no C++-pinned packet golden uses only carried kinds; the harness would prove nothing")
	}
	sort.Slice(carried, func(i, j int) bool { return carried[i].name < carried[j].name })
	for _, pin := range carried {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			u := load(pin.paths)
			pruned := rootClosure(u, pin.root)
			files, err := Generate(pruned)
			if err != nil {
				t.Fatalf("generate %s: %v", pin.root, err)
			}
			base := pruned.DeclFile[pin.root]
			intField, boolField, intMin, intMax := topLevelFields(pruned, pin.root)
			probe := packetProbe(pruned, pin.root, base, intField, boolField, intMin, intMax)
			pinBytes, err := os.ReadFile(filepath.Join(root, "testdata", "wire", pin.name+".bin"))
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
					got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
						"main.py":   []byte(probe),
						"input.bin": pinBytes,
					})))
					if got != "ok" {
						t.Fatalf("%s under %s:\n%s", pin.name, run.name, got)
					}
					out, err := pyrun.Analyze(t, dir, pruned.Package)
					if err != nil || !strings.Contains(out, "Success: no issues found") {
						t.Fatalf("%s under %s: mypy --strict: %v\n%s", pin.name, run.name, err, out)
					}
				})
			}
		})
	}
	var names []string
	for name := range skipped {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Logf("skip %s: %s", name, skipped[name])
	}
}

// TestPythonPacketCoreNegativeControl edits a copy of a generated reader and
// requires the packet gate to go red by name.
func TestPythonPacketCoreNegativeControl(t *testing.T) {
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
	text := string(files[module])
	if !strings.Contains(text, "self.health = ") {
		t.Fatalf("the generated ShipCreate reader has no health assignment:\n%s", text)
	}
	marker := "self.health = _s_ri.value"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated ShipCreate reader has no health assignment:\n%s", text)
	}
	text = text[:at] + "self.health = 1 + " + text[at+len("self.health = "):]
	files[module] = []byte(text)
	pinBytes, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := packetProbe(pruned, "ShipCreate", base, "health", "has_flags", big.NewInt(0), big.NewInt(1000))
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":   []byte(probe),
		"input.bin": pinBytes,
	})
	if err == nil || !strings.Contains(out, "FAILED: write-back differs") {
		t.Fatalf("the planted reader edit did not go red on the write-back marker (err %v):\n%s", err, out)
	}
}

// TestPythonPacketCoreSurface holds the class surface every field kind maps
// to: constants, enum and flags name tables, a constructor at each default
// (strings, bytes and flags included), equality by value and a readable repr.
func TestPythonPacketCoreSurface(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "test", "packet-defaults", "Defaults.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Defaults.schema"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	extra := "package packetdefaults\n\nconst SurfaceMax = 7\n\nenum Color { Red, Blue }\n\ntype Floats\n{\n    f32 float32\n    f64 float64\n}\n"
	if err := os.WriteFile(filepath.Join(src, "Surface.schema"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	files := map[string][]byte{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		files[u.Package+"/"+f.Base+".py"] = []byte(emitModuleMode(u, f, true))
		bases = append(bases, f.Base)
	}
	files[u.Package+"/__init__.py"] = []byte(packageInit(u, bases))
	files[u.Package+"/py.typed"] = []byte{}
	runtime, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range runtime {
		files[u.Package+"/"+rel] = data
	}
	probe := pyrun.ImportPrelude + `
import sys
from packetdefaults import Batch, Caps, Choice, ChoiceType, Color, Conditional, Floats, Sample, SurfaceMax

if SurfaceMax != 7 or Color.None_ != 0 or Color.Red != 1 or Color.Blue != 2 or Color.Max != 2:
    print("FAILED: a constant or enum class")
    sys.exit(1)
if Color.NAMES != {0: "None", 1: "Red", 2: "Blue"}:
    print("FAILED: the enum name table")
    sys.exit(1)
if Caps.Jump != 1 or Caps.Crouch != 2 or Caps.Fly != 4 or Caps.Count != 3:
    print("FAILED: the flags masks")
    sys.exit(1)
if Caps.NAMES != {0: "Jump", 1: "Crouch", 2: "Fly"}:
    print("FAILED: the flags name table")
    sys.exit(1)
s = Sample()
if s.name != "é𐀀" or s.token != b"\x5cn\x5ct" or s.caps != (Caps.Jump | Caps.Fly):
    print("FAILED: string, bytes and flags defaults")
    sys.exit(1)
if s.empty_name != "" or s.empty_token != b"" or s.empty_caps != 0:
    print("FAILED: empty defaults")
    sys.exit(1)
if not (s == Sample()) or Sample() != Sample():
    print("FAILED: equality by value")
    sys.exit(1)
s.name = "x"
if s == Sample():
    print("FAILED: equality ignores a changed field")
    sys.exit(1)
if "Sample(" not in repr(Sample()) or "name=" not in repr(Sample()):
    print("FAILED: repr")
    sys.exit(1)
b = Batch()
if len(b.items) != 2 or len(b.counted) != 1:
    print("FAILED: array defaults")
    sys.exit(1)
if b.items[0] is b.items[1] or b.counted[0] is b.items[0]:
    print("FAILED: array elements share one object")
    sys.exit(1)
if b.counted[0].name != "é𐀀" or b.counted[0].caps != (Caps.Jump | Caps.Fly):
    print("FAILED: array elements are not at their defaults")
    sys.exit(1)
c = Choice()
if c.type != 0 or not isinstance(c.sample, Sample) or not isinstance(c.conditional, Conditional):
    print("FAILED: union construction")
    sys.exit(1)
if ChoiceType.None_ != 0 or ChoiceType.Sample != 1 or ChoiceType.Conditional != 2 or ChoiceType.Max != 2:
    print("FAILED: union tag class")
    sys.exit(1)
if not (c == Choice()):
    print("FAILED: union equality")
    sys.exit(1)
f = Floats()
if f.f32 != 0.0 or f.f64 != 0.0 or not (f == Floats()):
    print("FAILED: float defaults or equality")
    sys.exit(1)
f.f32 = -0.0
if f == Floats():
    print("FAILED: float equality is not by bits")
    sys.exit(1)
print("ok")
`
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
			if got != "ok" {
				t.Fatalf("class surface under %s:\n%s", run.name, got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, u.Package)
	if err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("class surface mypy --strict: %v\n%s", err, out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestPythonBranchReadLocalsAreUnique: a method binds its typed Ref[bool] and
// Ref[int] holders once at entry, and every numbered temporary (_s_v1, ...) is
// bound once across both arms of an if/else and past the branch so mypy sees a
// single type per local.
func TestPythonBranchReadLocalsAreUnique(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	schema := `package branch

type Branchy
{
    active bool
    if active
    {
        yes bool = true
        yes_i int8 = 5
    }
    else
    {
        no int16 = 77
    }
    tail int8 = -3
}
`
	if err := os.WriteFile(filepath.Join(src, "Branch.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[u.Package+"/Branch.py"])
	for _, method := range []string{"    def _s_read(", "    def _s_validate("} {
		at := strings.Index(text, method)
		if at < 0 {
			t.Fatalf("the generated type lacks %s:\n%s", strings.TrimSpace(method), text)
		}
		body := text[at:]
		if next := strings.Index(body[1:], "\n    def "); next >= 0 {
			body = body[:next+1]
		}
		if strings.Count(body, "_s_rb = Ref(False)") > 1 || strings.Count(body, "_s_ri = Ref(0)") > 1 {
			t.Errorf("%s rebinds a Ref holder:\n%s", strings.TrimSpace(method), body)
		}
		assigns := regexp.MustCompile(`(?m)^\s+(_s_v\d+) = `).FindAllStringSubmatch(body, -1)
		if len(assigns) < 3 {
			t.Fatalf("%s binds %d numbered temporaries, want at least three:\n%s", strings.TrimSpace(method), len(assigns), body)
		}
		seen := map[string]bool{}
		for _, m := range assigns {
			if seen[m[1]] {
				t.Errorf("%s binds %s twice across branches:\n%s", strings.TrimSpace(method), m[1], body)
			}
			seen[m[1]] = true
		}
	}
	probe := pyrun.ImportPrelude + `
import sys
from branch import Branchy

a = Branchy()
a.active = True
a.yes = False
a.yes_i = -12
a.no = 999
a.tail = 42
ra = Branchy.read(a.write())
if not ra.active or ra.yes is not False or ra.yes_i != -12 or ra.no != 0 or ra.tail != 42:
    print("FAILED: active=True branch round-trip or SPEC §5 untaken zero:", repr(ra))
    sys.exit(1)

b = Branchy()
b.active = False
b.no = -321
b.tail = -8
rb = Branchy.read(b.write())
if rb.active or rb.yes is not False or rb.yes_i != 0 or rb.no != -321 or rb.tail != -8:
    print("FAILED: active=False branch round-trip or SPEC §5 untaken zero:", repr(rb))
    sys.exit(1)
print("ok")
`
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
			if got != "ok" {
				t.Fatalf("branch round-trip under %s:\n%s", run.name, got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, u.Package)
	if err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("mypy --strict on Branch: %v\n%s", err, out)
	}
}

// TestPythonPacketCoreEdgeCases exercises one-field and zero-field slots,
// empty and void-interleaved unions, zero-bit fields inside branches,
// module/declaration name sharing, sibling-import naming in list
// comprehensions, deterministic multi-file package init, and full-width
// integer boundaries.
func TestPythonPacketCoreEdgeCases(t *testing.T) {
	t.Parallel()

	// Union arm whose exported tag name collides with <Union>Type.NAMES.
	badUnion := &ir.Unit{
		Package: "edge",
		Files: []*ir.File{{
			Base: "Edge",
			Decls: []ir.Decl{
				&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "NAMES"}}},
			},
		}},
	}
	if err := CheckNames(badUnion); err == nil || !strings.Contains(err.Error(), "EventType") {
		t.Fatalf("expected CheckNames refusal on EventType.NAMES collision, got %v", err)
	}

	src := t.TempDir()
	filesSrc := map[string]string{
		"i.schema": `package edge

type Item
{
    v int32 = 9
}
`,
		"Chat.schema": `package edge

enum EmptyEnum {}

flags OneFlag { Bit }

type Empty {}

type OneField
{
    only int32 = 17
}

union EmptyUnion {}

union MixedUnion
{
    ping
    first OneField
    pong
    second OneField
}

type DegenerateBranch
{
    on bool
    if on
    {
        pinned int32 = 5 | min = 5, max = 5
        ee EmptyEnum
        em Empty
    }
    of OneFlag = { Bit }
    tail uint8 = 3
}

type IntEdges
{
    i8   int8
    u8   uint8
    i16  int16
    u16  uint16
    i32  int32
    u32  uint32
    i64  int64
    u64  uint64
}

type Chat
{
    item Item
    one OneField
    empty Empty
    eu EmptyUnion
    mu MixedUnion
    deg DegenerateBranch
    edges IntEdges
}
`,
	}
	for name, content := range filesSrc {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u := loadPythonUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Reversing u.Files must emit byte-identical output, __init__.py included.
	rev := *u
	rev.Files = slices.Clone(u.Files)
	slices.Reverse(rev.Files)
	filesRev, err := Generate(&rev)
	if err != nil {
		t.Fatalf("Generate reversed: %v", err)
	}
	for rel, data := range files {
		if string(filesRev[rel]) != string(data) {
			t.Fatalf("%s differs when u.Files order reverses", rel)
		}
	}

	// Surface emission with fixed-array comprehension referencing sibling i.schema.
	arrSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(arrSrc, "i.schema"), []byte(filesSrc["i.schema"]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arrSrc, "Holder.schema"), []byte("package edge\n\ntype Holder\n{\n    items [2]Item\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arrUnit := loadPythonUnit(t, arrSrc)
	arrMod := emitModuleMode(arrUnit, arrUnit.Files[0], true) + emitModuleMode(arrUnit, arrUnit.Files[1], true)
	if !strings.Contains(arrMod, "_s_mod_i.Item()") {
		t.Fatalf("expected _s_mod_i alias in array comprehension:\n%s", arrMod)
	}

	probe := pyrun.ImportPrelude + fmt.Sprintf(`
import sys
import edge
from edge import (
    Chat,
    DegenerateBranch,
    Empty,
    EmptyUnion,
    IntEdges,
    InvalidValue,
    Item,
    MixedUnion,
    MixedUnionType,
    OneField,
    PROTOCOL_ID,
)

if PROTOCOL_ID != 0x%016x:
    print("FAILED: PROTOCOL_ID mismatch")
    sys.exit(1)

# 1-field and 0-field __slots__ enforcement
o = OneField()
if o.only != 17 or OneField.read(o.write()) != o:
    print("FAILED: OneField round-trip")
    sys.exit(1)
try:
    o.extra = 1  # type: ignore[attr-defined]
except AttributeError:
    pass
else:
    print("FAILED: OneField __slots__ did not reject undeclared attribute")
    sys.exit(1)

e = Empty()
if Empty.read(e.write()) != e or repr(e) != "Empty()":
    print("FAILED: Empty round-trip or repr")
    sys.exit(1)

# EmptyUnion read, write, and invalid tag refusal
eu = EmptyUnion()
if EmptyUnion.read(eu.write()) != eu:
    print("FAILED: EmptyUnion round-trip")
    sys.exit(1)
for bad_tag in (1, -1, True):
    eu_bad = EmptyUnion()
    eu_bad.type = bad_tag  # type: ignore[assignment]
    try:
        eu_bad.write()
    except InvalidValue:
        pass
    else:
        print("FAILED: EmptyUnion accepted invalid tag", repr(bad_tag))
        sys.exit(1)

# Void-interleaved union equality and wire round-trip
u1 = MixedUnion()
u1.type = MixedUnionType.First
u1.first.only = 10
u2 = MixedUnion()
u2.type = MixedUnionType.First
u2.first.only = 20
if u1 == u2:
    print("FAILED: MixedUnion equality ignored payload difference after void arm")
    sys.exit(1)
u2.first.only = 10
if not (u1 == u2) or MixedUnion.read(u1.write()) != u1:
    print("FAILED: MixedUnion First round-trip")
    sys.exit(1)

u_void = MixedUnion()
u_void.type = MixedUnionType.Pong
if MixedUnion.read(u_void.write()) != u_void:
    print("FAILED: MixedUnion Pong void-arm round-trip")
    sys.exit(1)

# Degenerate zero-bit branch round-trip and validation
d_on = DegenerateBranch()
d_on.on = True
d_on.tail = 99
d_on_rt = DegenerateBranch.read(d_on.write())
if not d_on_rt.on or d_on_rt.pinned != 5 or d_on_rt.tail != 99:
    print("FAILED: DegenerateBranch on=True round-trip:", repr(d_on_rt))
    sys.exit(1)
d_on.pinned = 6
try:
    d_on.write()
except InvalidValue:
    pass
else:
    print("FAILED: DegenerateBranch accepted out-of-range constant-range field")
    sys.exit(1)

d_off = DegenerateBranch()
d_off.on = False
d_off.tail = 44
d_off_rt = DegenerateBranch.read(d_off.write())
if d_off_rt.on or d_off_rt.pinned != 0 or d_off_rt.tail != 44:
    print("FAILED: DegenerateBranch on=False SPEC §5 zeroing:", repr(d_off_rt))
    sys.exit(1)

# Full-width integer min and max round-trips
lo = IntEdges()
lo.i8 = -128
lo.u8 = 0
lo.i16 = -32768
lo.u16 = 0
lo.i32 = -(1 << 31)
lo.u32 = 0
lo.i64 = -(1 << 63)
lo.u64 = 0
if IntEdges.read(lo.write()) != lo:
    print("FAILED: IntEdges min boundary round-trip")
    sys.exit(1)

hi = IntEdges()
hi.i8 = 127
hi.u8 = 255
hi.i16 = 32767
hi.u16 = 65535
hi.i32 = (1 << 31) - 1
hi.u32 = (1 << 32) - 1
hi.i64 = (1 << 63) - 1
hi.u64 = (1 << 64) - 1
if IntEdges.read(hi.write()) != hi:
    print("FAILED: IntEdges max boundary round-trip")
    sys.exit(1)

# Composite Chat round-trip
c = Chat()
c.item.v = -42
c.one.only = 1234
c.mu.type = MixedUnionType.Second
c.mu.second.only = -99
c.deg.on = True
c.edges = hi
if Chat.read(c.write()) != c:
    print("FAILED: Chat composite round-trip")
    sys.exit(1)

print("ok")
`, u.ProtocolId)

	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
			if got != "ok" {
				t.Fatalf("edge cases under %s:\n%s", run.name, got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, u.Package)
	if err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("edge cases mypy --strict: %v\n%s", err, out)
	}
}
