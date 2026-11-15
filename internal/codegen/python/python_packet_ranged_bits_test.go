package python

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
		var bad []string
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

// rangedBitsCardKinds are the field kinds a golden this card reads may use:
// the packet core's and this card's own (bits; ranged integers are the int
// kind with a range). A golden that also uses a kind another packet card
// carries is that card's, and the close card reads every golden.
var rangedBitsCardKinds = map[string]bool{
	"bool": true, "int": true, "enum": true, "flags": true, "type": true, "union": true,
	"bits": true,
}

// rangedBitsForeignKind names the first kind in a root's closure that is
// neither the core's nor this card's, or "" when there is none.
func rangedBitsForeignKind(u *ir.Unit, root string) string {
	seen := map[string]bool{}
	var items func([]ir.Item) string
	var field func(*ir.Field) string
	field = func(f *ir.Field) string {
		kind := fieldKind(f)
		if kind == "" {
			return blockingKind(f)
		}
		if !rangedBitsCardKinds[kind] {
			return kind
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if ref != nil && !seen[ref.Name] {
				seen[ref.Name] = true
				return items(ref.Items)
			}
		case *ir.Union:
			if ref != nil && !seen[ref.Name] {
				seen[ref.Name] = true
				for _, v := range ref.Variants {
					if v.Void() {
						continue
					}
					if k := field(v.F); k != "" {
						return k
					}
				}
			}
		}
		return ""
	}
	items = func(list []ir.Item) string {
		for _, item := range list {
			switch item := item.(type) {
			case *ir.FieldItem:
				if k := field(item.F); k != "" {
					return k
				}
			case *ir.Branch:
				if k := items(item.Then); k != "" {
					return k
				}
				if k := items(item.Else); k != "" {
					return k
				}
			case *ir.ConstItem:
				return "const"
			case *ir.ReservedItem:
				return "reserved"
			case *ir.AlignItem:
				return "align"
			default:
				return fmt.Sprintf("%T", item)
			}
		}
		return ""
	}
	st := u.Structs[root]
	if st == nil {
		return "missing root"
	}
	seen[root] = true
	return items(st.Items)
}

// rangedBitsGoldenPins returns every single-root golden whose closure uses
// only the core's kinds and this card's and declares a ranged integer or bits
// field. Every other golden is skipped by the kind that blocks it, named.
func rangedBitsGoldenPins(t *testing.T) (picked []packetPin, skipped map[string]string) {
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
	skipped = map[string]string{}
	for _, pin := range packetPins() {
		if pin.note != "" {
			skipped[pin.name] = pin.note
			continue
		}
		u := load(pin.paths)
		if rootBlocker(u, pin.root) == "bits" {
			t.Errorf("%s is still skipped for bits, which the codec now carries (docs/PORT-PYTHON.md, THE PACKET CORE CUT)", pin.name)
			continue
		}
		if foreign := rangedBitsForeignKind(u, pin.root); foreign != "" {
			skipped[pin.name] = foreign
			continue
		}
		if len(rangedBitsFields(u, pin.root)) == 0 {
			skipped[pin.name] = "no ranged integer or bits field"
			continue
		}
		picked = append(picked, pin)
	}
	return picked, skipped
}

// TestPythonPacketRangedBits reads and writes back every carried golden that
// uses a ranged integer or bits field under both pinned interpreters, checks
// every out-of-contract write and every declared boundary, refuses truncated
// and hostile input, and holds the kind's class defaults (docs/PORT-PYTHON.md,
// THE VALUE MAPPING).
func TestPythonPacketRangedBits(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	pins, skipped := rangedBitsGoldenPins(t)
	names := make([]string, 0, len(pins))
	for _, pin := range pins {
		names = append(names, pin.name)
	}
	sort.Strings(names)
	want := []string{"bench_bits", "bench_ints", "probebits"}
	for _, name := range want {
		if !slices.Contains(names, name) {
			t.Fatalf("the ranged/bits golden set is %v, want %s carried (docs/PORT-PYTHON.md, THE PACKET CORE CUT)", names, name)
		}
	}
	skippedNames := make([]string, 0, len(skipped))
	for name := range skipped {
		skippedNames = append(skippedNames, name)
	}
	sort.Strings(skippedNames)
	for _, name := range skippedNames {
		t.Logf("skip %s: %s", name, skipped[name])
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
			pinBytes, err := os.ReadFile(pinFile(root, pin.name))
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
		if err := os.WriteFile(filepath.Join(src, "RangedBits.schema"), []byte(rangedBitsFixture), 0o644); err != nil {
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

	t.Run("go-reference", func(t *testing.T) {
		t.Parallel()
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "RangedBits.schema"), []byte(rangedBitsFixture), 0o644); err != nil {
			t.Fatal(err)
		}
		u := loadPythonUnit(t, src)
		want := rangedBitsGoReference(t, u, root)
		for _, c := range rangedBitsCases {
			if _, ok := want[c.name]; !ok {
				t.Fatalf("the Go reference wrote no bytes for case %s", c.name)
			}
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate synthetic unit: %v", err)
		}
		var cases strings.Builder
		for _, c := range rangedBitsCases {
			fmt.Fprintf(&cases, "%s|%x\n", c.name, want[c.name])
		}
		probe := rangedBitsReferenceProbe(u.Package)
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
					"cases.txt": []byte(cases.String()),
				})))
				if got != "ok" {
					t.Fatalf("the Go reference cases under %s:\n%s", run.name, got)
				}
			})
		}
	})
}

// rangedBitsFixture is the card's own small schema: each kind alone, the two
// together, and both beside the core's bool, bare integer and nested type.
const rangedBitsFixture = `package rangedbits

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

type Inner
{
    level uint8 = 2 | min = 2, max = 9
    tag   bits(5)
}

type WithCore
{
    alive bool
    raw   int32
    inner Inner
    wide  int64 = -5000000000 | min = -5000000000, max = 5000000000
    full  uint64 | min = 0, max = 18446744073709551615
    b64   bits(64)
    b2    bits(2) = 2
}
`

// rangedBitsCase is one value of the fixture: a type born at its defaults,
// then each dotted field path set to a decimal literal or a bool.
type rangedBitsCase struct {
	name string
	typ  string
	set  [][2]string
}

// rangedBitsCases cover every type at its defaults, each range's minimum and
// maximum, an interior value, every bits width class (1, 2, 3, 5, 9, 33 and
// 64), the int32 and int64 ranged paths and the full-range uint64 path.
var rangedBitsCases = []rangedBitsCase{
	{"rangeonly-default", "RangeOnly", nil},
	{"rangeonly-max", "RangeOnly", [][2]string{{"value", "5"}}},
	{"padonly-default", "PadOnly", nil},
	{"padonly-max", "PadOnly", [][2]string{{"small", "7"}}},
	{"bitsonly-default", "BitsOnly", nil},
	{"bitsonly-max", "BitsOnly", [][2]string{{"small", "5"}, {"wide", "18446744073709551615"}}},
	{"defaults-default", "Defaults", nil},
	{"defaults-max", "Defaults", [][2]string{{"ranged", "4"}, {"marker", "511"}}},
	{"mixed-default", "Mixed", nil},
	{"mixed-min", "Mixed", [][2]string{{"ri", "-3"}, {"u32", "0"}, {"u64", "1"}, {"b1", "0"}, {"b9", "0"}, {"b33", "0"}, {"b64", "0"}}},
	{"mixed-max", "Mixed", [][2]string{{"ri", "4"}, {"u32", "4294967295"}, {"u64", "18446744073709551615"}, {"b1", "1"}, {"b9", "511"}, {"b33", "8589934591"}, {"b64", "18446744073709551615"}}},
	{"mixed-mid", "Mixed", [][2]string{{"ri", "0"}, {"u32", "2863311530"}, {"u64", "9223372036854775808"}, {"b1", "1"}, {"b9", "257"}, {"b33", "4294967296"}, {"b64", "12345678901234567890"}}},
	{"degenerate-default", "Degenerate", nil},
	{"degenerate-max", "Degenerate", [][2]string{{"b", "15"}}},
	{"inner-default", "Inner", nil},
	{"withcore-default", "WithCore", nil},
	{"withcore-min", "WithCore", [][2]string{{"alive", "false"}, {"raw", "-2147483648"}, {"inner.level", "2"}, {"inner.tag", "0"}, {"wide", "-5000000000"}, {"full", "0"}, {"b64", "0"}, {"b2", "0"}}},
	{"withcore-max", "WithCore", [][2]string{{"alive", "true"}, {"raw", "2147483647"}, {"inner.level", "9"}, {"inner.tag", "31"}, {"wide", "5000000000"}, {"full", "18446744073709551615"}, {"b64", "18446744073709551615"}, {"b2", "3"}}},
	{"withcore-mid", "WithCore", [][2]string{{"alive", "true"}, {"raw", "-1"}, {"inner.level", "5"}, {"inner.tag", "17"}, {"wide", "-1"}, {"full", "9223372036854775807"}, {"b64", "1"}, {"b2", "1"}}},
}

// rangedBitsGoReference generates the fixture with the Go emitter, runs a
// probe that writes every case with the generated Go writer (and reads it
// back with the generated Go reader), and returns each case's bytes.
func rangedBitsGoReference(t *testing.T, u *ir.Unit, root string) map[string][]byte {
	t.Helper()
	slowtest.Gate(t, "the generated Go reference")
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("the Go emitter refused the fixture: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	mod := fmt.Sprintf("module %s\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n",
		u.Package, slowtest.SerializeGo(t, root))
	var oracle strings.Builder
	fmt.Fprintf(&oracle, "package %s\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n\n\t\"github.com/mas-bandwidth/serialize.go/v2\"\n)\n\n", u.Package)
	oracle.WriteString("func TestRangedBitsGoReference(t *testing.T) {\n")
	for _, c := range rangedBitsCases {
		// the Go emitter births a type with a declared default through
		// New<T>, and every other type is its zero value
		hasNew := false
		for _, data := range files {
			if strings.Contains(string(data), "func New"+c.typ+"() "+c.typ+" {") {
				hasNew = true
			}
		}
		if hasNew {
			fmt.Fprintf(&oracle, "\t{\n\t\tv := New%s()\n", c.typ)
		} else {
			fmt.Fprintf(&oracle, "\t{\n\t\tvar v %s\n", c.typ)
		}
		for _, s := range c.set {
			parts := strings.Split(s[0], ".")
			for i, p := range parts {
				parts[i] = goname.Member(p)
			}
			fmt.Fprintf(&oracle, "\t\tv.%s = %s\n", strings.Join(parts, "."), s[1])
		}
		fmt.Fprintf(&oracle, "\t\tw := serialize.NewWriteStream(make([]byte, 64))\n")
		fmt.Fprintf(&oracle, "\t\tif err := Write%s(w, &v); err != nil {\n\t\t\tt.Fatalf(\"%s: write: %%v\", err)\n\t\t}\n", c.typ, c.name)
		fmt.Fprintf(&oracle, "\t\tw.Flush()\n")
		fmt.Fprintf(&oracle, "\t\tvar back %s\n", c.typ)
		fmt.Fprintf(&oracle, "\t\tif err := Read%s(serialize.NewReadStream(w.Data()), &back); err != nil || back != v {\n\t\t\tt.Fatalf(\"%s: read back: %%v\", err)\n\t\t}\n", c.typ, c.name)
		fmt.Fprintf(&oracle, "\t\tfmt.Printf(\"CASE|%s|%%x\\n\", w.Data())\n\t}\n", c.name)
	}
	oracle.WriteString("}\n")
	writeTree(t, dir, map[string][]byte{
		"go.mod":            []byte(mod),
		"reference_test.go": []byte(oracle.String()),
	})
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "300s", "-run", "^TestRangedBitsGoReference$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go reference: %v\n%s", err, out)
	}
	got := map[string][]byte{}
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "CASE|")
		if !ok {
			continue
		}
		name, hexText, ok := strings.Cut(rest, "|")
		if !ok {
			t.Fatalf("the Go reference line %q is not name|hex", line)
		}
		data, err := hex.DecodeString(hexText)
		if err != nil {
			t.Fatalf("the Go reference line %q carries no hex: %v", line, err)
		}
		got[name] = data
	}
	return got
}

// rangedBitsReferenceProbe builds each case in Python, requires its write to
// equal the Go reference's bytes and the Go reference's bytes to read back to
// the same value, which also holds every declared default to the Go New*.
func rangedBitsReferenceProbe(pkg string) string {
	var b strings.Builder
	b.WriteString(pyrun.ImportPrelude)
	types := map[string]bool{}
	for _, c := range rangedBitsCases {
		types[c.typ] = true
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "import sys\nfrom %s import %s\n\n", pkg, strings.Join(names, ", "))
	b.WriteString("want = {}\nwith open(\"cases.txt\") as _s_cases:\n    for line in _s_cases:\n        name, _, text = line.strip().partition(\"|\")\n        want[name] = bytes.fromhex(text)\n\n")
	b.WriteString("def check(name, value, cls):\n")
	b.WriteString("    got = value.write()\n")
	b.WriteString("    if got != want[name]:\n        print(\"FAILED: \" + name + \" writes \" + got.hex() + \", the Go reference writes \" + want[name].hex())\n        sys.exit(1)\n")
	b.WriteString("    back = cls.read(want[name])\n")
	b.WriteString("    if back != value:\n        print(\"FAILED: \" + name + \" reads the Go reference bytes as \" + repr(back))\n        sys.exit(1)\n\n")
	for _, c := range rangedBitsCases {
		fmt.Fprintf(&b, "v = %s()\n", c.typ)
		for _, s := range c.set {
			value := s[1]
			switch value {
			case "true":
				value = "True"
			case "false":
				value = "False"
			}
			fmt.Fprintf(&b, "v.%s = %s\n", s[0], value)
		}
		fmt.Fprintf(&b, "check(%q, v, %s)\n", c.name, c.typ)
	}
	b.WriteString("print(\"ok\")\n")
	return b.String()
}

// TestPythonPacketRangedBitsNegativeControl edits a copy of the generated
// ProbeBits reader so its first bits field lands one too low, and requires
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

// TestPythonPacketRangedBitsGoReferenceNegativeControl moves the declared
// minimum of WithCore's int64 range by one in a copy of the generated Python
// module (the width is unchanged, so only the offset moves), and requires the
// go-reference probe to go red on its Go reference marker.
func TestPythonPacketRangedBitsGoReferenceNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "RangedBits.schema"), []byte(rangedBitsFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	want := rangedBitsGoReference(t, u, root)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	module := u.Package + "/" + u.DeclFile["WithCore"] + ".py"
	text := string(files[module])
	marker := "-5000000000, 5000000000"
	if !strings.Contains(text, marker) {
		t.Fatalf("the generated WithCore module names no %s range:\n%s", marker, text)
	}
	files[module] = []byte(strings.ReplaceAll(text, marker, "-5000000001, 5000000000"))
	var cases strings.Builder
	for _, c := range rangedBitsCases {
		fmt.Fprintf(&cases, "%s|%x\n", c.name, want[c.name])
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":   []byte(rangedBitsReferenceProbe(u.Package)),
		"cases.txt": []byte(cases.String()),
	})
	if err == nil || !strings.Contains(out, ", the Go reference writes ") {
		t.Fatalf("the planted range edit did not go red on the Go reference marker (err %v):\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
}
