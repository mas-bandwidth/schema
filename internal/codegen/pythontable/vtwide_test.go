package pythontable

// THE WIDE SCALARS on the Python table wire (ROADMAP.md, Variable tables,
// wide-scalars; docs/SPEC-TABLES.md §3, §15): int128, uint128, fixed(I, F) and
// ufixed(I, F) ride the integer codec as their raw storage integers. This file
// proves the kind on Go-engine constructs — built as luatable's
// luaVTConstructs are — that use it alone and beside the kinds the table core
// already carries (bool, the integers, bits, by-value tables and *T pointers),
// in a field, a nested table and a union-free pointer closure. The manifest
// rows that mix kinds from many cards are the py-vt-*-rows cards' work.
//
// Each construct is one read on read-then-write: the Go engine writes the
// canonical form-1 wire of a JSON sample, the strict Python read loads it, and
// the save must be the same bytes with measure equal to the length, the loaded
// value unchanged by the save, under both pinned interpreters. A wire whose
// wide payload is outside the declared range must refuse by name, and a write
// outside the range must raise InvalidValue and leave the caller's value
// untouched. TestPythonVtWideNegativeControl moves the int128 width in a copy
// of the emitted runtime and requires every construct to go red.

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type wideVTConstruct struct{ name, schema, root, json string }

// wideVTConstructs use the card's kinds alone and with the base's kinds. The
// fixed-point values are the exact raw-scaled decimals the Go engine reads and
// writes; a bounded array, a map or a union is another card's construct.
var wideVTConstructs = []wideVTConstruct{
	{
		"wide_field_alone",
		`package wide

table Wide
{
    x int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    y uint128
    f fixed(48, 16) | min = -100000, max = 100000
    g ufixed(8, 8)  | min = 0, max = 200
}
`,
		"Wide",
		`{"x":"-170141183460469231731687303715884105728","y":"340282366920938463463374607431768211455","f":"-12345.5","g":3.25}`,
	},
	{
		"wide_with_base_kinds",
		`package wide

table Mix
{
    b bool
    i int32
    u uint64
    h bits(40)
    x int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    y uint128
    f fixed(48, 16) | min = -100000, max = 100000
    g ufixed(8, 8)  | min = 0, max = 200
}
`,
		"Mix",
		`{"b":true,"i":-7,"u":"18446744073709551615","h":"1099511627775","x":"-170141183460469231731687303715884105728","y":"340282366920938463463374607431768211455","f":"-12345.5","g":3.25}`,
	},
	{
		"wide_every_width",
		`package wide

table Widths
{
    f8   fixed(8, 0)    | min = -128, max = 127
    f16  fixed(16, 0)   | min = -32768, max = 32767
    f32  fixed(32, 0)   | min = -2147483648, max = 2147483647
    f64  fixed(64, 0)   | min = -9223372036854775808, max = 9223372036854775807
    f128 fixed(112, 16) | min = -1000000, max = 1000000
    u8   ufixed(8, 0)   | min = 0, max = 255
    u16  ufixed(16, 0)  | min = 0, max = 65535
    u32  ufixed(32, 0)  | min = 0, max = 4294967295
    u64  ufixed(64, 0)  | min = 0, max = 9223372036854775807
    u128 ufixed(112, 16) | min = 0, max = 2000000
    i    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    u    uint128
}
`,
		"Widths",
		`{"f8":-7,"f16":1234,"f32":-123456,"f64":"1099511627779","f128":"-999999.5","u8":250,"u16":65000,"u32":4000000000,"u64":"4611686018427387911","u128":"1999999.5","i":"123456789012345678901234567890","u":"123456789012345678901234567890"}`,
	},
	{
		"wide_nested_table",
		`package wide

table Inner
{
    x int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    y fixed(112, 16) | min = -1000000, max = 1000000
}

table Outer
{
    tag   bool
    inner Inner
    u     uint128
}
`,
		"Outer",
		`{"tag":true,"inner":{"x":"123456789012345678901234567890","y":"-12.25"},"u":"12345678901234567890"}`,
	},
	{
		"wide_pointer_closure",
		`package wide

table Node
{
    v    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    f    fixed(60, 4) | min = -1000000000000, max = 1000000000000
    next *Node
}

table Root
{
    head *Node
    n    uint128
}
`,
		"Root",
		`{"head":{"v":"-170141183460469231731687303715884105728","f":"-1.5","next":{"v":"1","f":"2.25"}},"n":"340282366920938463463374607431768211455"}`,
	},
}

// wideEngineWire is the Go engine's form-1 wire of the JSON text at root.
func wideEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
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
	wire, err := tablewire.EncodeVariable(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

// wideProbeSource is the strict read-then-write probe: the save must be the
// wire it read, measure must agree with it, and the save must not change the
// loaded value.
func wideProbeSource(pkg, root string) string {
	return pyrun.ImportPrelude + `from ` + pkg + ` import ` + root + `

with open("wire", "rb") as handle:
    wire = handle.read()
value = ` + root + `.load(wire)
before = repr(value)
saved = value.save()
if saved != wire:
    print("diff", len(saved), len(wire))
elif value.measure() != len(wire):
    print("measure", value.measure(), len(wire))
elif repr(value) != before:
    print("mutated")
else:
    print("same", len(wire))
`
}

// wideRefusalSource is the refusal probe: a wide payload outside its declared
// range refuses by name on read, and a wide value outside its range raises
// InvalidValue on write without changing the caller's value.
func wideRefusalSource(pkg, root, field string) string {
	return pyrun.ImportPrelude + `from ` + pkg + ` import InvalidValue, ` + root + `, Refused

with open("wire", "rb") as handle:
    wire = handle.read()
try:
    ` + root + `.load(wire)
    print("read")
except Refused as err:
    print("refused", err.reason)

value = ` + root + `()
value.` + field + ` = 11
before = value.` + field + `
try:
    value.save()
    print("wrote")
except InvalidValue as err:
    print("invalid", err.path, value.` + field + ` == before)

value.` + field + ` = 10
data = value.save()
print("after", ` + root + `.load(data) == value, value.measure() == len(data))
`
}

// leBytes is a signed value's little-endian two's-complement payload at width
// n bytes, the table wire's order.
func wideLeBytes(value *big.Int, n int) []byte {
	m := new(big.Int).Set(value)
	if m.Sign() < 0 {
		m.Add(m, new(big.Int).Lsh(big.NewInt(1), uint(8*n)))
	}
	be := m.FillBytes(make([]byte, n))
	out := make([]byte, n)
	for i := range out {
		out[i] = be[n-1-i]
	}
	return out
}

// wideNarrowWire is a form-1 file of one Narrow field carrying 11, outside the
// declared range [-10, 10].
func wideNarrowWire(t *testing.T, u *ir.Unit, root, field string) []byte {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	var f *ir.Field
	for _, candidate := range st.Fields {
		if candidate.Name == field {
			f = candidate
		}
	}
	if f == nil {
		t.Fatalf("%s has no field %s", root, field)
	}
	body := []byte{1, byte(ir.TableWireFieldKind(f))}
	body = append(body, wideLeBytes(big.NewInt(11), 16)...)
	body = append(body, 0)
	id := make([]byte, 8)
	new(big.Int).SetUint64(ir.TableFieldWireId(f)).FillBytes(id)
	for i, j := 0, len(id)-1; i < j; i, j = i+1, j-1 {
		id[i], id[j] = id[j], id[i]
	}
	body = append(body, id...)
	body = append(body, wideLeBytes(big.NewInt(1), 8)...)
	wire := append([]byte{1}, body...)
	return wire
}

// wideCellFailures generates each construct (its emitted files edited by
// mutate when set), runs the read-then-write probe under both pins, and
// returns the findings and the number of probe runs that held.
func wideCellFailures(t *testing.T, mutate func(map[string][]byte)) (fails []string, held int) {
	t.Helper()
	for _, c := range wideVTConstructs {
		u := loadUnit(t, c.schema)
		wire := wideEngineWire(t, u, c.root, c.json)
		files, err := Generate(u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refuses it: %v", c.name, err))
			continue
		}
		if mutate != nil {
			mutate(files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		main := wideProbeSource(u.Package, c.root)
		want := fmt.Sprintf("same %d", len(wire))
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{
			{"current", pyrun.Interpreter},
			{"floor", pyrun.Floor},
		} {
			stdout, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{
				"main.py": []byte(main),
				"wire":    wire,
			})
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s (%s): the probe went red: %v\n%s", c.name, pin.name, err, strings.TrimSpace(stderr)))
				continue
			}
			held++
			if got := strings.TrimSpace(stdout); got != want {
				fails = append(fails, fmt.Sprintf("%s (%s): %q, want %q", c.name, pin.name, got, want))
			}
		}
		if mutate == nil {
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				fails = append(fails, fmt.Sprintf("%s: mypy --strict over the generated package:\n%s", c.name, out))
			}
			if out, err := pyrun.Analyze(t, dir, u.Package+"/_schema"); err != nil {
				fails = append(fails, fmt.Sprintf("%s: mypy --strict over the runtime:\n%s", c.name, out))
			}
		}
	}
	return fails, held
}

// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWide(t *testing.T) {
	t.Parallel()
	fails, held := wideCellFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if held == 0 {
		t.Fatal("the wide constructs held nothing")
	}
	if held != 2*len(wideVTConstructs) {
		t.Fatalf("the wide constructs held %d probe runs, want %d", held, 2*len(wideVTConstructs))
	}
}

// TestPythonVtWideRefused holds the one read's other verdict: an out-of-range
// wide payload refuses by name, and an out-of-range write raises InvalidValue
// before writing a byte and leaves the caller's value untouched.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWideRefused(t *testing.T) {
	t.Parallel()
	const schema = `package wide

table Narrow
{
    x int128 | min = -10, max = 10
}
`
	u := loadUnit(t, schema)
	wire := wideNarrowWire(t, u, "Narrow", "x")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	main := wideRefusalSource(u.Package, "Narrow", "x")
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{
		{"current", pyrun.Interpreter},
		{"floor", pyrun.Floor},
	} {
		stdout, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{
			"main.py": []byte(main),
			"wire":    wire,
		})
		if err != nil {
			t.Fatalf("%s: the refusal probe went red: %v\n%s", pin.name, err, stderr)
		}
		for _, line := range []string{
			"refused out_of_range",
			"invalid x True",
			"after True True",
		} {
			if !strings.Contains(stdout, line+"\n") {
				t.Errorf("%s: the probe output lacks %q:\n%s", pin.name, line, stdout)
			}
		}
	}
}

// TestPythonVtWideNegativeControl plants, one at a time, a defect in the real
// runtime a copy carries: the integer codec's int128 width (every construct
// must go red under both pins, so no construct is green by holding nothing) and
// the id table's numbering order (the nested table must go red, because the
// save then renumbers its fields). Each control's finding names the construct
// and the pin.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtWideNegativeControl(t *testing.T) {
	t.Parallel()
	plant := func(file, from, to string) func(map[string][]byte) {
		return func(files map[string][]byte) {
			src := string(files[file])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(src, from))
			}
			files[file] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	for _, ctl := range []struct {
		name   string
		mutate func(map[string][]byte)
		all    bool
	}{
		{
			"int128-width",
			plant("wide/_schema/kind_int.py", "    18: 16,", "    18: 8,"),
			true,
		},
		{
			"nested-id-order",
			plant("wide/_schema/kind_table.py",
				"        w.varint(ctx.idref(fd.field_id))\n        w.byte(fd.kind)\n        self.write_element(w, fd, value, ctx)",
				"        from .save import write_body\n\n        body = bytearray()\n        write_body(_body_writer(body), value, ctx, False)\n        w.varint(ctx.idref(fd.field_id))\n        w.byte(fd.kind)\n        w.varint(len(body))\n        w.raw(body)"),
			false,
		},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := wideCellFailures(t, ctl.mutate)
			if len(fails) == 0 {
				t.Fatalf("%s: NEGATIVE CONTROL FAILED: every wide construct stayed green", ctl.name)
			}
			if ctl.all {
				want := 2 * len(wideVTConstructs)
				if len(fails) != want {
					t.Fatalf("%s: NEGATIVE CONTROL FAILED: %d constructs went red, want %d (every construct under both pins):\n%s", ctl.name, len(fails), want, strings.Join(fails, "\n"))
				}
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), fails[0])
		})
	}
}
