package python

import (
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// usesFloatKind reports whether a root's by-value closure has a float32,
// float64 or fixed point field.
func usesFloatKind(u *ir.Unit, root string) bool {
	seen := map[string]bool{}
	var walk func(name string) bool
	walk = func(name string) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		var fields []*ir.Field
		if st, ok := u.Structs[name]; ok {
			fields = st.Fields
		} else if un, ok := u.Unions[name]; ok {
			for _, v := range un.Variants {
				if !v.Void() {
					fields = append(fields, v.F)
				}
			}
		}
		for _, f := range fields {
			switch f.Type.Kind {
			case ir.TFloat32, ir.TFloat64, ir.TFixed:
				return true
			case ir.TNamed:
				if walk(f.Type.Name) {
					return true
				}
			}
		}
		return false
	}
	return walk(root)
}

// floatsContractSchema declares every float kind the packet wire carries:
// both raw widths, a compressed float32, signed, unsigned, degenerate and
// 128-bit fixed point, beside the core's bool and uint8, most with a declared
// default the class is born at.
const floatsContractSchema = `package contract

type Probe
{
    f32  float32 = 0.1
    flag bool = true
    f64 float64 = -0.0
    cf  float32 = 1.25 | min = -5, max = 5, resolution = 0.001
    q   fixed(16, 16) = 1.5 | min = -180, max = 180
    u   ufixed(8, 8) | min = 0, max = 200
    d   fixed(16, 16) = 3 | min = 3, max = 3
    w   fixed(112, 16) | min = -1000000, max = 1000000
    tick uint8
}
`

// floatsContractProbe holds the writer's contract and the reader's refusals
// on contract.Probe. Every line it prints that is not "ok" names a vector.
// layout is the wire, field by field, as (name, bits), so the probe can lay
// a hostile pattern into one field without a writer.
func floatsContractProbe(layout []fieldBits) string {
	var rows []string
	for _, l := range layout {
		rows = append(rows, fmt.Sprintf("(%q, %d)", l.name, l.bits))
	}
	return pyrun.ImportPrelude + `
import math
import struct
import sys
from contract.Contract import Probe
from contract import InvalidValue, Refused
from contract._schema.serialize import Ref, WriteStream

LAYOUT = [` + strings.Join(rows, ", ") + `]
WANT = "` + floatsContractWire(layout) + `"
failed = []


def fail(name, detail):
    failed.append(name)
    print("FAILED: " + name + ": " + detail)


def f32(bits):
    return struct.unpack("<f", struct.pack("<I", bits))[0]


def bits32(v):
    return struct.unpack("<I", struct.pack("<f", v))[0]


def bits64(v):
    return struct.unpack("<Q", struct.pack("<d", v))[0]


fresh = Probe()
if bits32(fresh.f32) != bits32(0.1) or fresh.f32 != f32(bits32(0.1)):
    fail("default/f32", "f32 is not born at 0.1 rounded to float32: " + repr(fresh.f32))
if bits64(fresh.f64) != 0x8000000000000000:
    fail("default/f64", "f64 is not born at -0.0: " + repr(fresh.f64))
if fresh.cf != 1.25 or fresh.q != 3 << 15 or fresh.u != 0 or fresh.d != 3 << 16 or fresh.w != 0:
    fail("default/others", repr(fresh))


def refused_write(name, field, value):
    v = Probe()
    setattr(v, field, value)
    try:
        wire = v.write()
    except InvalidValue as exc:
        if exc.path != "Probe." + field:
            fail(name, "refused at " + exc.path + ", want Probe." + field)
        return
    except Exception as exc:
        fail(name, "raised " + type(exc).__name__ + ": " + str(exc))
        return
    fail(name, "accepted " + repr(value) + " as " + wire.hex())


nan = float("nan")
inf = float("inf")
q_max = 180 << 16
u_max = 200 << 8
w_max = 1000000 << 16
for name, field, value in [
    ("f32/write/nan", "f32", nan),
    ("f32/write/negative-nan", "f32", -nan),
    ("f32/write/inf", "f32", inf),
    ("f32/write/negative-inf", "f32", -inf),
    ("f32/write/narrows-to-inf", "f32", 1e39),
    ("f32/write/negative-narrows-to-inf", "f32", -1e39),
    ("f32/write/int", "f32", 1),
    ("f32/write/bool", "f32", True),
    ("f32/write/str", "f32", "1.0"),
    ("f32/write/none", "f32", None),
    ("f64/write/nan", "f64", nan),
    ("f64/write/inf", "f64", inf),
    ("f64/write/negative-inf", "f64", -inf),
    ("f64/write/int", "f64", 1),
    ("f64/write/bool", "f64", False),
    ("cf/write/nan", "cf", nan),
    ("cf/write/inf", "cf", inf),
    ("cf/write/above-max", "cf", 5.01),
    ("cf/write/below-min", "cf", -5.01),
    ("cf/write/next-float32-above-max", "cf", f32(bits32(5.0) + 1)),
    ("cf/write/int", "cf", 1),
    ("q/write/above-max", "q", q_max + 1),
    ("q/write/below-min", "q", -q_max - 1),
    ("q/write/float", "q", 1.5),
    ("q/write/bool", "q", True),
    ("u/write/negative", "u", -1),
    ("u/write/above-max", "u", u_max + 1),
    ("d/write/off-the-one-value", "d", (3 << 16) + 1),
    ("d/write/zero", "d", 0),
    ("w/write/above-max", "w", w_max + 1),
    ("w/write/below-min", "w", -w_max - 1),
]:
    refused_write(name, field, value)


def round_trip(name, edit, check):
    v = Probe()
    edit(v)
    try:
        wire = v.write()
    except InvalidValue as exc:
        fail(name, "refused an in-contract value: " + str(exc))
        return
    try:
        got = Probe.read(wire)
    except Refused as exc:
        fail(name, "refused its own write: " + exc.reason)
        return
    if got.write() != wire:
        fail(name, "the read value does not write back byte-identical")
        return
    if not check(got):
        fail(name, "read back " + repr(got))


round_trip("f32/roundtrip/rounds", lambda v: setattr(v, "f32", 0.1), lambda g: g.f32 == f32(bits32(0.1)))
round_trip("f32/roundtrip/rounds-down-to-max", lambda v: setattr(v, "f32", f32(0x7F7FFFFF) * (1 + 2.0**-26)),
           lambda g: bits32(g.f32) == 0x7F7FFFFF)
round_trip("f32/roundtrip/negative-zero", lambda v: setattr(v, "f32", -0.0), lambda g: bits32(g.f32) == 0x80000000)
round_trip("f32/roundtrip/subnormal", lambda v: setattr(v, "f32", f32(1)), lambda g: bits32(g.f32) == 1)
round_trip("f64/roundtrip/max", lambda v: setattr(v, "f64", 1.7976931348623157e308), lambda g: g.f64 == 1.7976931348623157e308)
round_trip("f64/roundtrip/subnormal", lambda v: setattr(v, "f64", 5e-324), lambda g: g.f64 == 5e-324)
round_trip("cf/roundtrip/min", lambda v: setattr(v, "cf", -5.0), lambda g: g.cf == -5.0)
round_trip("cf/roundtrip/max", lambda v: setattr(v, "cf", 5.0), lambda g: g.cf == 5.0)
round_trip("cf/roundtrip/step", lambda v: setattr(v, "cf", 1.2344), lambda g: abs(g.cf - 1.234) < 0.0005)
round_trip("q/roundtrip/min", lambda v: setattr(v, "q", -q_max), lambda g: g.q == -q_max)
round_trip("q/roundtrip/max", lambda v: setattr(v, "q", q_max), lambda g: g.q == q_max)
round_trip("q/roundtrip/fraction", lambda v: setattr(v, "q", -1), lambda g: g.q == -1)
round_trip("u/roundtrip/max", lambda v: setattr(v, "u", u_max), lambda g: g.u == u_max)
round_trip("w/roundtrip/min", lambda v: setattr(v, "w", -w_max), lambda g: g.w == -w_max)
round_trip("w/roundtrip/max", lambda v: setattr(v, "w", w_max), lambda g: g.w == w_max)


def raw(patterns):
    s = WriteStream(64)
    for field, bits in LAYOUT:
        if bits:
            s.serialize_bits(Ref(patterns.get(field, 0)), bits)
    s.flush()
    return s.data()


def refused_read(name, wire, reason):
    try:
        Probe.read(wire)
    except Refused as exc:
        if reason and exc.reason != reason:
            fail(name, "refused with " + exc.reason + ", want " + reason)
        return
    except Exception as exc:
        fail(name, "the reader raised " + type(exc).__name__ + ": " + str(exc))
        return
    fail(name, "read accepted " + wire.hex())


base = {"f32": bits32(1.0), "f64": bits64(1.0), "q": q_max, "u": 0, "w": w_max}
try:
    Probe.read(raw(base))
except Refused as exc:
    fail("read/base", "the hostile layout's base was refused: " + exc.reason)
for name, patterns, reason in [
    ("f32/read/nan", {"f32": 0x7FC00000}, "non_finite_float"),
    ("f32/read/inf", {"f32": 0x7F800000}, "non_finite_float"),
    ("f32/read/negative-inf", {"f32": 0xFF800000}, "non_finite_float"),
    ("f64/read/nan", {"f64": 0x7FF8000000000001}, "non_finite_float"),
    ("f64/read/inf", {"f64": 0x7FF0000000000000}, "non_finite_float"),
    ("cf/read/headroom", {"cf": (1 << dict(LAYOUT)["cf"]) - 1}, "value_out_of_range"),
    ("q/read/headroom", {"q": (1 << dict(LAYOUT)["q"]) - 1}, "value_out_of_range"),
    ("u/read/headroom", {"u": (1 << dict(LAYOUT)["u"]) - 1}, "value_out_of_range"),
    ("w/read/headroom", {"w": (1 << dict(LAYOUT)["w"]) - 1}, "value_out_of_range"),
]:
    p = dict(base)
    p.update(patterns)
    refused_read(name, raw(p), reason)

v = Probe()
v.f32, v.f64, v.cf, v.q, v.u, v.w, v.tick = -1.5, 2.25, 0.0, -1, 12345, -7, 200
try:
    wire = v.write().hex()
except InvalidValue as exc:
    wire = "refused: " + str(exc)
if wire != WANT:
    fail("wire/layout", "wrote " + wire + ", want " + WANT)

good = Probe().write()
for n in range(len(good)):
    refused_read("truncated/" + str(n), good[:n], "")
refused_read("trailing", good + b"\x00", "")

if not failed:
    print("ok")
`
}

// floatsContractWire is the wire of the probe's layout value, laid out here
// in Go from SPEC §4.3 alone and not by any runtime: each field's bits in
// declaration order, least significant first, in ceil(bits / 8) bytes. The
// float32 and float64 ride their IEEE-754 patterns, the compressed float its
// step index (0.0 over [-5, 5] at 0.001 is step 5000 of 10000), and each fixed
// point field its raw value's offset from min << F.
func floatsContractWire(layout []fieldBits) string {
	values := map[string]*big.Int{
		"f32":  new(big.Int).SetUint64(uint64(math.Float32bits(-1.5))),
		"f64":  new(big.Int).SetUint64(math.Float64bits(2.25)),
		"cf":   big.NewInt(5000),
		"flag": big.NewInt(1),
		"tick": big.NewInt(200),
		"q":    big.NewInt(-1 + 180<<16),
		"u":    big.NewInt(12345),
		"d":    big.NewInt(0),
		"w":    big.NewInt(-7 + 1000000<<16),
	}
	acc := new(big.Int)
	var at uint
	for _, l := range layout {
		acc.Or(acc, new(big.Int).Lsh(values[l.name], at))
		at += uint(l.bits)
	}
	out := make([]byte, (at+7)/8)
	for i := range out {
		out[i] = byte(new(big.Int).Rsh(acc, uint(8*i)).Uint64())
	}
	return hex.EncodeToString(out)
}

type fieldBits struct {
	name string
	bits int64
}

// floatsContractLayout is contract.Probe's wire, field by field, worked by
// hand from SPEC §4.3: 32 and 64 bits for the raw floats with the core's bool
// (one bit) between them and its uint8 (eight) last; 10 / 0.001 = 10000
// steps in 14 bits; fixed(16, 16) over [-180, 180] spans 360 << 16 in 25 bits,
// ufixed(8, 8) over [0, 200] spans 200 << 8 in 16, the degenerate range costs
// none, and fixed(112, 16) over [-1e6, 1e6] spans 2e6 << 16 in 37. The
// generator's own widths (its write buffer capacity) must agree.
func floatsContractLayout(t *testing.T, u *ir.Unit) []fieldBits {
	t.Helper()
	want := []fieldBits{{"f32", 32}, {"flag", 1}, {"f64", 64}, {"cf", 14}, {"q", 25}, {"u", 16}, {"d", 0}, {"w", 37}, {"tick", 8}}
	st, ok := u.Structs["Probe"]
	if !ok {
		t.Fatal("the contract unit has no Probe")
	}
	m := &pyModule{u: u}
	var got []fieldBits
	for _, f := range st.Fields {
		got = append(got, fieldBits{f.Name, m.fieldBits(f, map[string]bool{})})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the generator's field widths are %v, want %v", got, want)
	}
	return want
}

func floatsContractUnit(t *testing.T) *ir.Unit {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Contract.schema"), []byte(floatsContractSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadPythonUnit(t, src)
}

// runBoth runs a probe over a generated tree under both pinned interpreters
// and the strict analyzer, and fails t naming every vector the probe printed.
func runBoth(t *testing.T, files map[string][]byte, pkg, probe string, inputs map[string][]byte) {
	t.Helper()
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			in := map[string][]byte{"main.py": []byte(probe)}
			maps.Copy(in, inputs)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", in)))
			if got != "ok" {
				t.Fatalf("under %s:\n%s", run.name, got)
			}
			out, err := pyrun.Analyze(t, dir, pkg)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("under %s: mypy --strict: %v\n%s", run.name, err, out)
			}
		})
	}
}

// sabotage returns a copy of files with each old string, which must occur
// exactly once in file rel, replaced by its new string.
func sabotage(t *testing.T, files map[string][]byte, rel string, edits ...[2]string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(files))
	maps.Copy(out, files)
	text, ok := out[rel]
	if !ok {
		t.Fatalf("sabotage: no %s in the generated tree", rel)
	}
	s := string(text)
	for _, e := range edits {
		if n := strings.Count(s, e[0]); n != 1 {
			t.Fatalf("sabotage: %s holds %d copies of %q, want exactly one", rel, n, e[0])
		}
		s = strings.Replace(s, e[0], e[1], 1)
	}
	out[rel] = []byte(s)
	return out
}

// TestPythonPacketFloats holds the float kinds on the Python packet wire
// (docs/PORT-PYTHON.md, THE NUMBER MODEL): every C++-pinned golden that uses
// float32, float64, a compressed float32 or fixed point and is carried whole
// reads and writes back byte-identical under both pinned interpreters; every
// out-of-contract write raises InvalidValue naming the field; every
// truncated or hostile input raises Refused; the packet-finite driver
// (test/packet-finite/python) passes; and each negative control, planted in
// a copy of the generated code or its runtime, goes red by name.
func TestPythonPacketFloats(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	t.Run("goldens", func(t *testing.T) {
		t.Parallel()
		units := map[string]*ir.Unit{}
		var carried []packetPin
		var blocked []string
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
			if !usesFloatKind(u, pin.root) {
				continue
			}
			if k := rootBlocker(u, pin.root); k != "" {
				blocked = append(blocked, pin.name+": "+k)
				continue
			}
			carried = append(carried, pin)
		}
		sort.Strings(blocked)
		for _, b := range blocked {
			t.Logf("float golden blocked by another kind: %s", b)
		}
		if len(carried) == 0 {
			t.Fatal("no C++-pinned golden that uses a float kind is carried whole; the gate would prove nothing")
		}
		for _, pin := range carried {
			u := units[strings.Join(pin.paths, "|")]
			t.Run(pin.name, func(t *testing.T) {
				t.Parallel()
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
				runBoth(t, files, pruned.Package, probe, map[string][]byte{"input.bin": pinBytes})
			})
		}
	})

	t.Run("contract", func(t *testing.T) {
		t.Parallel()
		u := floatsContractUnit(t)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		runBoth(t, files, u.Package, floatsContractProbe(floatsContractLayout(t, u)), nil)
	})

	finiteDir := filepath.Join(root, "test", "packet-finite", "python")
	finiteFiles := func(t *testing.T) (map[string][]byte, []byte) {
		t.Helper()
		u := loadPythonUnit(t, filepath.Join(finiteDir, "Finite.schema"))
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		driver, err := os.ReadFile(filepath.Join(finiteDir, "main.py"))
		if err != nil {
			t.Fatal(err)
		}
		return files, driver
	}

	t.Run("finite", func(t *testing.T) {
		t.Parallel()
		files, driver := finiteFiles(t)
		for _, run := range []struct {
			name string
			fn   func(testing.TB, string, string, map[string][]byte) []byte
		}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				gen := filepath.Join(dir, "generated")
				writeTree(t, gen, files)
				out := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": finiteMain(driver)})))
				if !strings.HasPrefix(out, "OK: packet finite Python") {
					t.Fatalf("under %s:\n%s", run.name, out)
				}
			})
		}
	})

	t.Run("negative-control", func(t *testing.T) {
		t.Parallel()
		// generated: the compressed float's range check is cut from a copy of
		// the generated validator. The runtime clamps an out-of-range value
		// to the declaration, so only the generated check refuses it.
		t.Run("generated", func(t *testing.T) {
			t.Parallel()
			u := floatsContractUnit(t)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			module := u.Package + "/Contract.py"
			text := string(files[module])
			at := strings.Index(text, "validate_compressed_float(_s_v")
			if at < 0 {
				t.Fatalf("the generated validator has no compressed float check:\n%s", text)
			}
			end := strings.Index(text[at:], "\n")
			files = sabotage(t, files, module, [2]string{text[at : at+end], "pass"})
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
				"main.py": []byte(floatsContractProbe(floatsContractLayout(t, u))),
			})
			want := []string{"cf/write/above-max", "cf/write/below-min", "cf/write/inf", "cf/write/int", "cf/write/nan", "cf/write/next-float32-above-max"}
			if got := failedVectors(out, "FAILED: "); err != nil || !slices.Equal(got, want) {
				t.Fatalf("the cut compressed range check failed %v (err %v), want exactly %v:\n%s", got, err, want, out)
			}
		})
		// fixed-read: the raw span check is cut from a copy of the bit
		// layer's fixed point read, so an offset in the bit headroom lands a
		// value outside the declaration.
		t.Run("fixed-read", func(t *testing.T) {
			t.Parallel()
			u := floatsContractUnit(t)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			files = sabotage(t, files, u.Package+"/_schema/serialize/_core.py",
				[2]string{"        if offset > raw_span:\n            return self._fail(VALUE_OUT_OF_RANGE)\n        ref.value = raw_min + offset\n",
					"        # SABOTAGED: the fixed point read takes every offset\n        ref.value = raw_min + offset\n"})
			dir := t.TempDir()
			writeTree(t, dir, files)
			out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
				"main.py": []byte(floatsContractProbe(floatsContractLayout(t, u))),
			})
			want := []string{"q/read/headroom", "u/read/headroom", "w/read/headroom"}
			if got := failedVectors(out, "FAILED: "); err != nil || !slices.Equal(got, want) {
				t.Fatalf("the cut fixed point span check failed %v (err %v), want exactly %v:\n%s", got, err, want, out)
			}
		})
		for _, dir := range []string{"read", "write"} {
			t.Run("finite-"+dir, func(t *testing.T) {
				t.Parallel()
				files, driver := finiteFiles(t)
				files = sabotageFinite(t, files, dir)
				work := t.TempDir()
				writeTree(t, filepath.Join(work, "generated"), files)
				_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), work, "main.py", map[string][]byte{"main.py": finiteMain(driver)})
				want, rerr := os.ReadFile(filepath.Join(finiteDir, dir+".expected"))
				if rerr != nil {
					t.Fatal(rerr)
				}
				got := failedVectors(stderr, "FAILED: ")
				if err == nil || strings.Join(got, "\n")+"\n" != string(want) {
					t.Fatalf("the %s sabotage failed (err %v):\n%s\nwant exactly test/packet-finite/python/%s.expected:\n%s",
						dir, err, strings.Join(got, "\n"), dir, want)
				}
			})
		}
	})
}

// finiteMain points the packet-finite driver at the generated tree beside it:
// the make target passes the directory as argv[1], and the test runs the
// driver from its work directory with generated/ next to it.
func finiteMain(driver []byte) []byte {
	return []byte("import os, sys\nsys.argv[1:] = [os.path.join(os.path.dirname(os.path.abspath(__file__)), \"generated\")]\n" + string(driver))
}

// sabotageFinite removes the finite rule from a copy of the generated tree in
// one direction: on read, the bit layer's non-finite pattern checks; on
// write, the field validators' finite checks and the bit layer's beside them.
func sabotageFinite(t *testing.T, files map[string][]byte, dir string) map[string][]byte {
	t.Helper()
	core := "finite/_schema/serialize/_core.py"
	if dir == "read" {
		return sabotage(t, files, core,
			[2]string{"        bits = self._take(32)\n        if (bits & 0x7F800000) == 0x7F800000:\n            return self._fail(NON_FINITE_FLOAT)\n",
				"        bits = self._take(32)\n        # SABOTAGED: the float32 read takes every pattern\n"},
			[2]string{"        bits = self._take(64)\n        if (bits & 0x7FF0000000000000) == 0x7FF0000000000000:\n            return self._fail(NON_FINITE_FLOAT)\n",
				"        bits = self._take(64)\n        # SABOTAGED: the float64 read takes every pattern\n"})
	}
	files = sabotage(t, files, core,
		[2]string{"    if not float_bits_are_finite(bits):\n        raise _misuse(f\"serialize_float: {value!r} is not finite\")\n",
			"    # SABOTAGED: the float32 write takes every pattern\n"},
		[2]string{"    if not double_bits_are_finite(bits):\n        raise _misuse(f\"serialize_double: {value!r} is not finite\")\n",
			"    # SABOTAGED: the float64 write takes every pattern\n"})
	return sabotage(t, files, "finite/_schema/__init__.py",
		[2]string{"    \"\"\"\n    _finite_float32(value, path)\n", "    \"\"\"\n    # SABOTAGED: the float32 field takes every float\n"},
		[2]string{"        raise InvalidValue(path, \"expected float\")\n    if not math.isfinite(value):\n        raise InvalidValue(path, f\"{value!r} is not finite\")\n\n\ndef validate_compressed_float",
			"        raise InvalidValue(path, \"expected float\")\n    # SABOTAGED: the float64 field takes every float\n\n\ndef validate_compressed_float"})
}

// failedVectors is the sorted set of vector names a probe printed as
// "<prefix><name>: <detail>".
func failedVectors(out, prefix string) []string {
	set := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			if name, _, ok := strings.Cut(rest, ": "); ok {
				set[name] = true
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
