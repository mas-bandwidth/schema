package python

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// arraysSchema holds fixed arrays [N]T and counted arrays [A..B]T of every
// element kind the packet core carries (bool, bare and ranged integers, enums,
// flags, types and unions), counted arrays with and without a minimum, arrays
// inside an if branch, and arrays as union arms.
const arraysSchema = `package arrays

enum Kind { A, B, C }

flags Bits { X, Y, Z }

type Elem
{
    on bool = true
    v int16 = -5
}

type Size
{
    n uint8
}

union Shape
{
    dot
    cube Elem
    size Size
}

type Many
{
    items [..2]Elem
}

type Pair
{
    v [2]uint8
}

union Holder
{
    many Many
    pair Pair
}

type Wrap
{
    h Holder
}

// the hand-checked vector: a = [1, 2], c = [7, 8] is 01 02 1d 20 00, the
// count riding as 1 in two bits, offset from its minimum of 1
type Small
{
    a [2]uint8
    c [1..3]uint8
}

type Arrays
{
    bs  [3]bool
    i8s [2]int8
    rs  [2]int32 | min = -10, max = 10
    ks  [2]Kind
    fs  [2]Bits
    es  [2]Elem
    us  [2]Shape
    ce  [..3]Elem
    cu  [1..2]Shape
    ci  [2..4]uint16
    on  bool
    if on
    {
        gated  [..2]Elem
        fixedg [2]Elem
    }
}
`

// arraysProbe reads, writes and refuses arrays under one pinned interpreter.
// Each failure prints a FAILED line naming what broke.
const arraysProbe = `
import sys
from arrays import Arrays, Bits, Elem, Holder, HolderType, InvalidValue, Kind, Refused, Shape, ShapeType, Small, Wrap


def fail(msg):
    print("FAILED: " + msg)
    sys.exit(1)


# the hand-checked vector, both ways
vec = bytes.fromhex("01021d2000")
s = Small()
s.a = [1, 2]
s.c = [7, 8]
if s.write() != vec:
    fail("the Small vector: wrote " + s.write().hex())
try:
    r = Small.read(vec)
except Refused as exc:
    fail("the Small vector was refused: " + exc.reason)
if r.a != [1, 2] or r.c != [7, 8]:
    fail("the Small vector read back " + repr(r))

# construction (SPEC 4.2): each element at its default, a counted array born
# at its minimum, no two elements one object
a = Arrays()
if a.bs != [False, False, False] or a.i8s != [0, 0] or a.rs != [0, 0] or a.ks != [0, 0] or a.fs != [0, 0]:
    fail("scalar array defaults")
if len(a.es) != 2 or a.es[0].on is not True or a.es[0].v != -5 or a.es[0] is a.es[1]:
    fail("type array defaults")
if len(a.us) != 2 or a.us[0].type != 0 or a.us[0] is a.us[1]:
    fail("union array defaults")
if a.ce != [] or len(a.cu) != 1 or a.cu[0].type != 0 or a.ci != [0, 0]:
    fail("a counted array is not born at its minimum")
if len(a.gated) != 0 or len(a.fixedg) != 2 or a.fixedg[0] is a.fixedg[1]:
    fail("branch array defaults")
# the branch is off at birth, so its arrays read back at the zero form;
# switched on, a value at its defaults round-trips whole
a.on = True
if Arrays.read(a.write()) != a:
    fail("a value at its defaults does not round-trip")
if Small.read(Small().write()) != Small():
    fail("a fresh Small does not round-trip")


def full():
    v = Arrays()
    v.bs = [True, False, True]
    v.i8s = [-128, 127]
    v.rs = [-10, 10]
    v.ks = [Kind.C, Kind.A]
    v.fs = [Bits.X | Bits.Z, Bits.Y]
    v.es[0].on = False
    v.es[0].v = 32767
    v.es[1].v = -32768
    box = Shape()
    box.type = ShapeType.Cube
    box.cube.v = 9
    size = Shape()
    size.type = ShapeType.Size
    size.size.n = 255
    v.us = [box, size]
    v.ce = [Elem(), Elem(), Elem()]
    v.ce[2].v = 1
    other = Shape()
    other.type = ShapeType.Dot
    v.cu = [other, box]
    v.ci = [1, 2, 3, 65535]
    v.on = True
    v.gated = [Elem(), Elem()]
    v.gated[1].on = False
    v.fixedg[1].v = 77
    return v


v = full()
data = v.write()
back = Arrays.read(data)
if back != v:
    fail("a full value does not round-trip: " + repr(back))
if back.write() != data:
    fail("write-back differs")
if back.ci != [1, 2, 3, 65535] or len(back.ce) != 3 or back.ce[2].v != 1 or back.cu[1].cube.v != 9:
    fail("counted arrays read back wrong: " + repr(back))

# an untaken branch reads as the SPEC 5 zero form: a counted array empty, a
# fixed array's elements at zero, never at their defaults
off = full()
off.on = False
got = Arrays.read(off.write())
if got.gated != [] or len(got.fixedg) != 2 or got.fixedg[0].on is not False or got.fixedg[0].v != 0 or got.fixedg[1].v != 0:
    fail("an untaken branch's arrays are not at the zero form: " + repr(got))

# the Go reference: each value below is built the same way in Go and
# written by the Go leg; Python writes the same bytes, and reads Go's bytes
# back to the same value


def small():
    s = Small()
    s.a = [1, 2]
    s.c = [7, 8]
    return s


def defaults():
    d = Arrays()
    d.on = True
    return d


def many():
    w = Wrap()
    w.h.type = HolderType.Many
    w.h.many.items = [Elem()]
    w.h.many.items[0].v = 12
    return w


def pair():
    w = Wrap()
    w.h.type = HolderType.Pair
    w.h.pair.v = [3, 250]
    return w


with open("go.hex") as handle:
    reference = [bytes.fromhex(line) for line in handle.read().split()]
cases = [("small", small()), ("defaults", defaults()), ("full", full()), ("off", off), ("many", many()), ("pair", pair())]
if len(reference) != len(cases):
    fail("the Go reference has %d values, want %d" % (len(reference), len(cases)))
for (name, value), want in zip(cases, reference):
    wrote = value.write()
    if wrote != want:
        fail("Go reference %s: python wrote %s, go wrote %s" % (name, wrote.hex(), want.hex()))
    try:
        read = type(value).read(want)
    except Refused as exc:
        fail("Go reference %s was refused: %s" % (name, exc.reason))
    if name != "off" and read != value:
        fail("Go reference %s read back as %r" % (name, read))
    if read.write() != want:
        fail("Go reference %s does not write back byte-identical" % name)

# arrays inside union arms
w = Wrap()
w.h.type = HolderType.Many
w.h.many.items = [Elem()]
w.h.many.items[0].v = 12
if Wrap.read(w.write()) != w:
    fail("a counted array in an arm does not round-trip")
w.h.type = HolderType.Pair
w.h.pair.v = [3, 250]
if Wrap.read(w.write()) != w:
    fail("a fixed array in an arm does not round-trip")
w.h.pair.v = [3]
try:
    w.write()
except InvalidValue as exc:
    if exc.path != "Wrap.h.pair.v":
        fail("a short arm array is refused at " + exc.path)
else:
    fail("a short arm array was written")


# every out-of-contract write raises InvalidValue at the element's path
def setitem(name, i, value):
    def f(v):
        getattr(v, name)[i] = value
    return f


def setattr_(name, value):
    def f(v):
        setattr(v, name, value)
    return f


def elem_v(name, i, value):
    def f(v):
        getattr(v, name)[i].v = value
    return f


def gated(value):
    def f(v):
        v.gated = value
    return f


def gated_v(v):
    v.gated[1].v = -40000


cases = [
    ("Arrays.bs", setattr_("bs", [True, False])),
    ("Arrays.bs", setattr_("bs", [True, False, True, True])),
    ("Arrays.bs", setattr_("bs", (True, False, True))),
    ("Arrays.bs", setattr_("bs", None)),
    ("Arrays.bs[1]", setitem("bs", 1, 1)),
    ("Arrays.i8s[0]", setitem("i8s", 0, 128)),
    ("Arrays.i8s[1]", setitem("i8s", 1, True)),
    ("Arrays.rs[1]", setitem("rs", 1, 11)),
    ("Arrays.rs[0]", setitem("rs", 0, -11)),
    ("Arrays.ks[0]", setitem("ks", 0, 4)),
    ("Arrays.fs[1]", setitem("fs", 1, 8)),
    ("Arrays.es[1]", setitem("es", 1, Shape())),
    ("Arrays.es[1].v", elem_v("es", 1, 40000)),
    ("Arrays.us[0]", setitem("us", 0, Elem())),
    ("Arrays.ce", setattr_("ce", [Elem(), Elem(), Elem(), Elem()])),
    ("Arrays.ce[0]", setattr_("ce", [None])),
    ("Arrays.cu", setattr_("cu", [])),
    ("Arrays.cu", setattr_("cu", [Shape(), Shape(), Shape()])),
    ("Arrays.ci", setattr_("ci", [1])),
    ("Arrays.ci", setattr_("ci", [1, 2, 3, 4, 5])),
    ("Arrays.ci[2]", setattr_("ci", [1, 2, -1])),
    ("Arrays.ci[1]", setattr_("ci", [1, 65536])),
    ("Arrays.gated", gated([Elem(), Elem(), Elem()])),
    ("Arrays.gated[1].v", gated_v),
]
for path, mutate in cases:
    v = full()
    mutate(v)
    try:
        v.write()
    except InvalidValue as exc:
        if exc.path != path:
            fail("refused at " + exc.path + ", want " + path)
    else:
        fail("an out-of-contract value was written at " + path)

# every truncation, a trailing byte, and a count past its bound are refused
for k in range(len(data)):
    try:
        Arrays.read(data[:k])
    except Refused:
        pass
    else:
        fail("a truncation to %d bytes was accepted" % k)
try:
    Arrays.read(data + b"\x00")
except Refused:
    pass
else:
    fail("a trailing byte was accepted")
try:
    Small.read(b"\x01\x02\x03" + bytes(6))
except Refused:
    pass
else:
    fail("a count above its bound was accepted")

# every single-bit flip either reads a value that writes back, or is refused
for i in range(len(data)):
    for bit in range(8):
        hostile = bytearray(data)
        hostile[i] ^= 1 << bit
        try:
            value = Arrays.read(bytes(hostile))
        except Refused:
            continue
        try:
            value.write()
        except InvalidValue as exc:
            fail("a flip at byte %d bit %d read an out-of-contract value: %s" % (i, bit, exc))

print("ok")
`

// arraysGoProbe builds the probe's reference values with the Go leg and
// prints each one's wire as a hex line, in the order the Python probe lists them.
const arraysGoProbe = `package main

import (
	"encoding/hex"
	"fmt"

	"github.com/mas-bandwidth/serialize.go/v2"

	"probe/arrays"
)

func emit(write func(*serialize.WriteStream) error) {
	stream := serialize.NewWriteStream(make([]byte, 4096))
	if err := write(stream); err != nil {
		panic(err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		panic(err)
	}
	fmt.Println(hex.EncodeToString(stream.Data()))
}

func full() arrays.Arrays {
	v := arrays.NewArrays()
	v.Bs = [3]bool{true, false, true}
	v.I8s = [2]int8{-128, 127}
	v.Rs = [2]int32{-10, 10}
	v.Ks = [2]arrays.Kind{arrays.KindC, arrays.KindA}
	v.Fs = [2]arrays.Bits{arrays.BitsX | arrays.BitsZ, arrays.BitsY}
	v.Es[0].On = false
	v.Es[0].V = 32767
	v.Es[1].V = -32768
	var box, size, other arrays.Shape
	box.Type = arrays.ShapeTypeCube
	box.Cube = arrays.NewElem()
	box.Cube.V = 9
	size.Type = arrays.ShapeTypeSize
	size.Size.N = 255
	other.Type = arrays.ShapeTypeDot
	v.Us = [2]arrays.Shape{box, size}
	v.CeCount = 3
	v.Ce[2].V = 1
	v.Cu[0], v.Cu[1], v.CuCount = other, box, 2
	v.Ci, v.CiCount = [4]uint16{1, 2, 3, 65535}, 4
	v.On = true
	v.GatedCount = 2
	v.Gated[1].On = false
	v.Fixedg[1].V = 77
	return v
}

func main() {
	small := arrays.NewSmall()
	small.A = [2]uint8{1, 2}
	small.C[0], small.C[1], small.CCount = 7, 8, 2
	emit(func(s *serialize.WriteStream) error { return arrays.WriteSmall(s, &small) })
	defaults := arrays.NewArrays()
	defaults.On = true
	emit(func(s *serialize.WriteStream) error { return arrays.WriteArrays(s, &defaults) })
	f := full()
	emit(func(s *serialize.WriteStream) error { return arrays.WriteArrays(s, &f) })
	off := full()
	off.On = false
	emit(func(s *serialize.WriteStream) error { return arrays.WriteArrays(s, &off) })
	var many arrays.Wrap
	many.H.Type = arrays.HolderTypeMany
	many.H.Many = arrays.NewMany()
	many.H.Many.ItemsCount = 1
	many.H.Many.Items[0].V = 12
	emit(func(s *serialize.WriteStream) error { return arrays.WriteWrap(s, &many) })
	var pair arrays.Wrap
	pair.H.Type = arrays.HolderTypePair
	pair.H.Pair.V = [2]uint8{3, 250}
	emit(func(s *serialize.WriteStream) error { return arrays.WriteWrap(s, &pair) })
}
`

// goReference generates the Go leg for the fixture, runs arraysGoProbe
// against the serialize.go runtime and returns its hex lines. It is behind
// the slow gate, because it shells out to the Go toolchain.
func goReference(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it builds the Go reference writer)")
	gen, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go reference: %v", err)
	}
	dir := t.TempDir()
	files := map[string][]byte{
		"go.mod":  []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", slowtest.SerializeGo(t, repoRoot(t)))),
		"main.go": []byte(arraysGoProbe),
	}
	for name, data := range gen {
		files["arrays/"+name] = data
	}
	writeTree(t, dir, files)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = string(ee.Stderr)
		}
		t.Fatalf("the Go reference: %v\n%s", err, msg)
	}
	return out
}

// arrayPins is every C++-pinned packet golden whose root uses an array,
// split into those the leg carries and those another kind still blocks.
func arrayPins(t *testing.T, root string) (carried []packetPin, blocked map[string]string, units map[string]*ir.Unit) {
	t.Helper()
	units = map[string]*ir.Unit{}
	blocked = map[string]string{}
	for _, pin := range packetPins() {
		if pin.note != "" {
			continue
		}
		key := strings.Join(pin.paths, "|")
		u, ok := units[key]
		if !ok {
			abs := make([]string, len(pin.paths))
			for i, p := range pin.paths {
				abs[i] = filepath.Join(root, filepath.FromSlash(p))
			}
			u = loadPythonUnit(t, abs...)
			units[key] = u
		}
		if !rootUsesArray(u, pin.root, map[string]bool{}) {
			continue
		}
		if k := rootBlocker(u, pin.root); k != "" {
			blocked[pin.name] = k
			continue
		}
		carried = append(carried, pin)
	}
	sort.Slice(carried, func(i, j int) bool { return carried[i].name < carried[j].name })
	return carried, blocked, units
}

func rootUsesArray(u *ir.Unit, name string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	if st, ok := u.Structs[name]; ok {
		for _, f := range st.Fields {
			if f.Array != ir.ArrayNone || (f.Type.Kind == ir.TNamed && rootUsesArray(u, f.Type.Name, seen)) {
				return true
			}
		}
	}
	if un, ok := u.Unions[name]; ok {
		for _, v := range un.Variants {
			if !v.Void() && (v.F.Array != ir.ArrayNone || rootUsesArray(u, v.F.Type.Name, seen)) {
				return true
			}
		}
	}
	return false
}

func generateArrays(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Arrays.schema"), []byte(arraysSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return u, files
}

// TestPythonPacketArrays holds fixed arrays [N]T and counted arrays [A..B]T
// (SPEC §4.6): the fixture's values write byte-identical to the Go leg's and
// read Go's bytes back to the same value under both pinned interpreters;
// every C++-pinned golden whose kinds are all arrays and the core's reads and
// writes back byte-identical, and every other array golden is listed with
// the kind it waits on; a hand-checked vector is written and read; every out-of-contract write
// raises InvalidValue at the element's path; every truncated or hostile input
// raises Refused; and the generated package is mypy-strict clean.
func TestPythonPacketArrays(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	carried, blocked, units := arrayPins(t, root)
	if len(carried) == 0 {
		t.Fatal("no C++-pinned packet golden uses an array and only carried kinds; the gate would prove nothing")
	}
	names := make([]string, 0, len(blocked))
	for name := range blocked {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if blocked[name] == "array" {
			t.Errorf("%s is blocked by the array kind itself", name)
		}
		t.Logf("array golden %s waits on %s", name, blocked[name])
	}

	u, files := generateArrays(t)
	text := string(files[u.Package+"/Arrays.py"])
	for _, want := range []string{
		"validate_array(",
		"for _s_v",
		"_s_serialize_int(_s_ri, 1, 2)",
		"_s_serialize_int(_s_ri, 2, 4)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the generated Arrays module lacks %q:\n%s", want, text)
		}
	}
	// the write buffer holds the widest value, every count at its bound: the
	// same size the Go leg publishes as <Type>MaxBytes
	gen, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	goText := string(gen["Arrays.go"])
	for _, name := range []string{"Small", "Arrays", "Many", "Wrap"} {
		want := fmt.Sprintf("const %sMaxBytes = %d\n", name, wireCapacity(&pyModule{u: u}, name))
		if !strings.Contains(goText, want) {
			t.Errorf("%s's write capacity differs from the Go leg's: want %q in\n%s", name, want, goText)
		}
	}

	interpreters := []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}}

	for _, pin := range carried {
		t.Run("golden/"+pin.name, func(t *testing.T) {
			t.Parallel()
			pruned := rootClosure(units[strings.Join(pin.paths, "|")], pin.root)
			gen, err := Generate(pruned)
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
			for _, run := range interpreters {
				t.Run(run.name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					writeTree(t, dir, gen)
					got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
						"main.py":   []byte(probe),
						"input.bin": pinBytes,
					})))
					if got != "ok" {
						t.Fatalf("%s under %s:\n%s", pin.name, run.name, got)
					}
				})
			}
			dir := t.TempDir()
			writeTree(t, dir, gen)
			out, err := pyrun.Analyze(t, dir, pruned.Package)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("%s: mypy --strict: %v\n%s", pin.name, err, out)
			}
		})
	}

	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		reference := goReference(t, u)
		for _, run := range interpreters {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
					"main.py": []byte(pyrun.ImportPrelude + arraysProbe),
					"go.hex":  reference,
				})))
				if got != "ok" {
					t.Fatalf("arrays under %s:\n%s", run.name, got)
				}
			})
		}
	})
	t.Run("mypy", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, err := pyrun.Analyze(t, dir, u.Package)
		if err != nil || !strings.Contains(out, "Success: no issues found") {
			t.Fatalf("arrays mypy --strict: %v\n%s", err, out)
		}
	})
}

// TestPythonPacketArraysNegativeControl drops the counted array's minimum
// from a copy of the generated reader, so the count no longer rides offset,
// and requires the arrays probe to go red on the hand-checked vector by name.
func TestPythonPacketArraysNegativeControl(t *testing.T) {
	t.Parallel()
	u, files := generateArrays(t)
	module := u.Package + "/Arrays.py"
	text := string(files[module])
	marker := "if not _s_serialize_int(_s_ri, 1, 3):"
	if strings.Count(text, marker) != 1 {
		t.Fatalf("the generated Small reader has %d count reads %q, want 1:\n%s", strings.Count(text, marker), marker, text)
	}
	files[module] = []byte(strings.Replace(text, marker, "if not _s_serialize_int(_s_ri, 0, 3):", 1))
	reference := goReference(t, u)
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py": []byte(pyrun.ImportPrelude + arraysProbe),
		"go.hex":  reference,
	})
	if err == nil || !strings.Contains(out, "FAILED: the Small vector") {
		t.Fatalf("the planted count edit did not go red on the Small vector (err %v):\n%s", err, out)
	}
}
