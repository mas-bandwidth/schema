package python

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rangedBitsField is one ranged integer or bits field in a root's closure, as
// a dotted Python attribute path and the contract its write must hold.
type rangedBitsField struct {
	path  string
	kind  string // "int" or "bits"
	width int
	min   *big.Int
	max   *big.Int
}

// rangedBitsFields walks a root struct's unconditional by-value fields and
// returns every ranged int and bits field, nested types included. Array
// elements, pointers, union arms and branch fields are left to their own
// cards; this card's kinds are scalars.
func rangedBitsFields(u *ir.Unit, root string) []rangedBitsField {
	st := u.Structs[root]
	if st == nil {
		return nil
	}
	var out []rangedBitsField
	var walk func(st *ir.Struct, prefix string, seen map[string]bool)
	walk = func(st *ir.Struct, prefix string, seen map[string]bool) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		defer delete(seen, st.Name)
		for _, item := range st.Items {
			item, ok := item.(*ir.FieldItem)
			if !ok || item.F.Array != ir.ArrayNone || item.F.Type.Pointer {
				continue
			}
			f := item.F
			path := prefix + f.Name
			switch f.Type.Kind {
			case ir.TInt:
				if f.HasIntRange {
					out = append(out, rangedBitsField{path: path, kind: "int", width: f.Type.Width, min: f.IntMin, max: f.IntMax})
				}
			case ir.TBits:
				out = append(out, rangedBitsField{
					path: path, kind: "bits", width: f.Type.Width,
					min: big.NewInt(0), max: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)),
				})
			case ir.TNamed:
				if ref, ok := f.Type.Ref.(*ir.Struct); ok {
					walk(ref, path+".", seen)
				}
			}
		}
	}
	walk(st, "", map[string]bool{})
	return out
}

// rangedBitsProbe reads one pinned golden, writes it back byte-identical,
// requires an InvalidValue for every out-of-contract write to a ranged or bits
// field, round-trips each declared boundary, and refuses a short read and a
// trailing byte (docs/PORT-PYTHON.md, THE HARNESS; SPEC §4.3, §5).
func rangedBitsProbe(u *ir.Unit, root, base string, fields []rangedBitsField) string {
	var b strings.Builder
	b.WriteString(pyrun.ImportPrelude)
	fmt.Fprintf(&b, "import sys\nfrom %s.%s import %s\nfrom %s import InvalidValue, Refused\n\n", u.Package, base, root, u.Package)
	fmt.Fprintf(&b, "with open(\"input.bin\", \"rb\") as _s_input:\n    data = _s_input.read()\n")
	fmt.Fprintf(&b, "value = %s.read(data)\n", root)
	fmt.Fprintf(&b, "again = value.write()\n")
	fmt.Fprintf(&b, "if again != data:\n")
	fmt.Fprintf(&b, "    print(\"FAILED: write-back differs\")\n    sys.exit(1)\n")
	for _, f := range fields {
		bad := []string{f.min.String(), f.max.String()}
		if f.kind == "int" {
			bad = []string{new(big.Int).Sub(f.min, big.NewInt(1)).String(), new(big.Int).Add(f.max, big.NewInt(1)).String()}
		} else {
			bad = []string{"-1", new(big.Int).Add(f.max, big.NewInt(1)).String()}
		}
		bad = append(bad, "True")
		fmt.Fprintf(&b, "old = value.%s\n", f.path)
		fmt.Fprintf(&b, "for bad in (%s):\n", strings.Join(bad, ", "))
		fmt.Fprintf(&b, "    value.%s = bad\n", f.path)
		fmt.Fprintf(&b, "    try:\n        value.write()\n    except InvalidValue:\n        pass\n    else:\n")
		fmt.Fprintf(&b, "        print(\"FAILED: %s accepted an out-of-contract write\")\n        sys.exit(1)\n", f.path)
		fmt.Fprintf(&b, "for good in (%s, %s):\n", f.min.String(), f.max.String())
		fmt.Fprintf(&b, "    value.%s = good\n", f.path)
		fmt.Fprintf(&b, "    try:\n        rt = %s.read(value.write())\n    except (InvalidValue, Refused) as exc:\n", root)
		fmt.Fprintf(&b, "        print(\"FAILED: %s boundary did not round-trip: \" + str(exc))\n        sys.exit(1)\n", f.path)
		fmt.Fprintf(&b, "    if rt.%s != good:\n        print(\"FAILED: %s boundary did not round-trip\")\n        sys.exit(1)\n", f.path, f.path)
		fmt.Fprintf(&b, "value.%s = old\n", f.path)
	}
	fmt.Fprintf(&b, "if len(data) > 0:\n")
	fmt.Fprintf(&b, "    try:\n        %s.read(data[:-1])\n    except Refused:\n        pass\n    else:\n", root)
	fmt.Fprintf(&b, "        print(\"FAILED: a short read was accepted\")\n        sys.exit(1)\n")
	fmt.Fprintf(&b, "try:\n    %s.read(data + b\"\\x00\")\n", root)
	fmt.Fprintf(&b, "except Refused:\n    pass\nelse:\n    print(\"FAILED: a trailing byte was accepted\")\n    sys.exit(1)\n")
	b.WriteString("print(\"ok\")\n")
	return b.String()
}

// rangedBitsGoldenPins returns every single-root golden whose kinds the codec
// carries and whose root closure declares a ranged integer or bits field.
func rangedBitsGoldenPins(t *testing.T) []packetPin {
	t.Helper()
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
	var picked []packetPin
	for _, pin := range packetPins() {
		if pin.note != "" {
			continue
		}
		u := load(pin.paths)
		blocker := rootBlocker(u, pin.root)
		if blocker == "bits" {
			t.Errorf("%s is still skipped for bits, which the codec now carries (docs/PORT-PYTHON.md, THE PACKET CORE CUT)", pin.name)
			continue
		}
		if blocker != "" {
			continue
		}
		if len(rangedBitsFields(u, pin.root)) == 0 {
			continue
		}
		picked = append(picked, pin)
	}
	return picked
}

// TestPythonPacketRangedBits reads and writes back every carried golden that
// uses a ranged integer or bits field under both pinned interpreters, checks
// every out-of-contract write and every declared boundary, refuses truncated
// and hostile input, and holds the kind's class defaults (docs/PORT-PYTHON.md,
// THE VALUE MAPPING).
func TestPythonPacketRangedBits(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	pins := rangedBitsGoldenPins(t)
	names := make([]string, 0, len(pins))
	for _, pin := range pins {
		names = append(names, pin.name)
	}
	sort.Strings(names)
	want := []string{"bench_bits", "bench_ints", "probebits"}
	for _, name := range want {
		if !strings.Contains(strings.Join(names, " "), name) {
			t.Fatalf("the ranged/bits golden set is %v, want %s carried (docs/PORT-PYTHON.md, THE PACKET CORE CUT)", names, name)
		}
	}

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

	for _, pin := range pins {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			u := load(pin.paths)
			pruned := rootClosure(u, pin.root)
			files, err := Generate(pruned)
			if err != nil {
				t.Fatalf("generate %s: %v", pin.root, err)
			}
			base := pruned.DeclFile[pin.root]
			probe := rangedBitsProbe(pruned, pin.root, base, rangedBitsFields(pruned, pin.root))
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

	t.Run("synthetic", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		schema := `package rangedbits

type RangeOnly
{
    value int8 | min = 0, max = 5
}

type PadOnly
{
    small bits(3)
}

type BitsOnly
{
    small bits(3)
    wide  bits(64)
}

type Defaults
{
    ranged int16 = -3 | min = -3, max = 4
    marker bits(9) = 300
}

type Mixed
{
    ri  int16  = -3 | min = -3, max = 4
    u32 uint32 | min = 0, max = 4294967295
    u64 uint64 = 1 | min = 1, max = 18446744073709551615
    b1  bits(1) = 1
    b9  bits(9) = 300
    b33 bits(33)
    b64 bits(64)
}

type Degenerate
{
    pinned int8 = 5 | min = 5, max = 5
    b      bits(4) = 3
}
`
		if err := os.WriteFile(filepath.Join(src, "RangedBits.schema"), []byte(schema), 0o644); err != nil {
			t.Fatal(err)
		}
		u := loadPythonUnit(t, src)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate synthetic unit: %v", err)
		}
		probe := pyrun.ImportPrelude + `
import sys
from rangedbits import BitsOnly, Degenerate, Defaults, InvalidValue, Mixed, PadOnly, RangeOnly, Refused

d = Defaults()
if d.ranged != -3 or d.marker != 300:
    print("FAILED: a ranged or bits default is not in the constructor")
    sys.exit(1)

m = Mixed()
m.ri = 4
m.u32 = 4294967295
m.u64 = 18446744073709551615
m.b1 = 1
m.b9 = 511
m.b33 = (1 << 33) - 1
m.b64 = (1 << 64) - 1
data = m.write()
r = Mixed.read(data)
if r != m or r.write() != data:
    print("FAILED: a mixed ranged/bits value did not round-trip")
    sys.exit(1)

for field, bad in (
    ("ri", -4), ("ri", 5),
    ("u32", -1), ("u32", 1 << 32),
    ("u64", 0), ("u64", 1 << 64),
    ("b1", -1), ("b1", 2),
    ("b9", -1), ("b9", 1 << 9),
    ("b33", -1), ("b33", 1 << 33),
    ("b64", -1), ("b64", 1 << 64),
):
    v = Mixed()
    setattr(v, field, bad)
    try:
        v.write()
    except InvalidValue:
        pass
    else:
        print("FAILED: " + field + " accepted an out-of-contract write")
        sys.exit(1)

for field, good in (
    ("ri", -3), ("ri", 4),
    ("u32", 0), ("u32", 4294967295),
    ("u64", 1), ("u64", 18446744073709551615),
    ("b1", 0), ("b1", 1),
    ("b9", 0), ("b9", 511),
    ("b33", 0), ("b33", (1 << 33) - 1),
    ("b64", 0), ("b64", (1 << 64) - 1),
):
    v = Mixed()
    setattr(v, field, good)
    if getattr(Mixed.read(v.write()), field) != good:
        print("FAILED: " + field + " boundary did not round-trip")
        sys.exit(1)

v = Mixed()
v.b9 = True
try:
    v.write()
except InvalidValue:
    pass
else:
    print("FAILED: a bool where bits(9) is declared was written")
    sys.exit(1)

for bad in (b"\x06", b""):
    try:
        RangeOnly.read(bad)
    except Refused:
        pass
    else:
        print("FAILED: a ranged offset above its span or a short read was accepted")
        sys.exit(1)
if RangeOnly.read(b"\x00") != RangeOnly():
    print("FAILED: the canonical minimum did not read")
    sys.exit(1)

try:
    PadOnly.read(b"\x08")
except Refused:
    pass
else:
    print("FAILED: nonzero padding was accepted")
    sys.exit(1)
if PadOnly.read(b"\x07").small != 7:
    print("FAILED: the canonical bits(3) maximum did not read")
    sys.exit(1)
try:
    PadOnly.read(b"\x07\x00")
except Refused:
    pass
else:
    print("FAILED: a trailing byte after bits(3) was accepted")
    sys.exit(1)

b = BitsOnly()
b.small = 5
b.wide = (1 << 64) - 1
data = b.write()
if BitsOnly.read(data) != b:
    print("FAILED: bits(3)+bits(64) did not round-trip")
    sys.exit(1)
for bad in (data[:-1], data + b"\x00", b""):
    try:
        BitsOnly.read(bad)
    except Refused:
        pass
    else:
        print("FAILED: a truncated or hostile bits input was accepted")
        sys.exit(1)

dg = Degenerate()
dg.pinned = 5
dg.b = 3
if Degenerate.read(dg.write()) != dg:
    print("FAILED: a degenerate ranged field did not round-trip")
    sys.exit(1)
dg.pinned = 6
try:
    dg.write()
except InvalidValue:
    pass
else:
    print("FAILED: a degenerate ranged field accepted another value")
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
					t.Fatalf("synthetic ranged/bits unit under %s:\n%s", run.name, got)
				}
				out, err := pyrun.Analyze(t, dir, u.Package)
				if err != nil || !strings.Contains(out, "Success: no issues found") {
					t.Fatalf("synthetic ranged/bits unit under %s: mypy --strict: %v\n%s", run.name, err, out)
				}
			})
		}
	})
}

// TestPythonPacketRangedBitsNegativeControl edits a copy of the generated
// ProbeBits reader so its first bits field lands one too high, and requires
// TestPythonPacketRangedBits's write-back marker to go red.
func TestPythonPacketRangedBitsNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	u := loadPythonUnit(t, filepath.Join(root, "examples"))
	pruned := rootClosure(u, "ProbeBits")
	files, err := Generate(pruned)
	if err != nil {
		t.Fatal(err)
	}
	base := pruned.DeclFile["ProbeBits"]
	module := pruned.Package + "/" + base + ".py"
	text := string(files[module])
	marker := "self.small = _s_ri.value"
	if strings.Count(text, marker) != 1 {
		t.Fatalf("the generated ProbeBits reader has %d small assignments, want one:\n%s", strings.Count(text, marker), text)
	}
	files[module] = []byte(strings.Replace(text, marker, "self.small = _s_ri.value - 1 if _s_ri.value > 0 else 0", 1))
	pinBytes, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "probebits.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := rangedBitsProbe(pruned, "ProbeBits", base, rangedBitsFields(pruned, "ProbeBits"))
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":   []byte(probe),
		"input.bin": pinBytes,
	})
	if err == nil || !strings.Contains(out, "FAILED: write-back differs") {
		t.Fatalf("the planted bits reader edit did not go red on the write-back marker (err %v):\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
}
