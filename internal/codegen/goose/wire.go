// The Goose PACKET WIRE (SPEC §4.3 – §4.13, §5, §6.1): the type surface's
// Write<T> and Read<T>, bit-packed over the vendored serialize.goose v2.1.1
// (runtime/serialize*.goose, pinned by runtime/SERIALIZE_PIN). The wire is the
// C++ reference's, byte for byte: every bit count is folded at generation
// time from the declaration, as the Go and C targets fold it, and each value
// moves through the runtime's own bit primitives.
//
// THE API (docs/PORT-GOOSE.md, THE PACKET WIRE):
//
//	fn Write<T>(s: WriteStream&, value: T&) -> i32
//	fn Read<T>(s: ReadStream&, value: T&) -> bool
//	fn Zero<T>(value: T&)
//	let <T>MaxBits: i64; let <T>MaxBytes: i64
//
// A write returns SERIALIZE_ERROR_NONE, or the refusal kind of the first
// value the wire cannot carry (SERIALIZE_ERROR_VALUE_OUT_OF_RANGE for a value
// outside its range, a count or a length past its bound, an out-of-set union
// tag; SERIALIZE_ERROR_NON_FINITE_FLOAT for a NaN or an infinity, SPEC §4.3).
// Goose has no build-removable assert (`assert` aborts in every build), so a
// writer contract is the every-build refusal Go makes native, never an abort:
// serialize.goose aborts on write misuse (its U9), so the generated writer
// checks every value BEFORE the runtime sees it.
//
// A read returns true, or false with the reason latched on the stream
// (serialize_error(s)): the runtime's own (past_end, non_finite_float,
// nonzero_padding) or the schema's (value_out_of_range for a value in the bit
// headroom, a const or reserved mismatch or a broken surrogate pair;
// invalid_utf8 and embedded_nul for string content), the reasons the C
// target latches. A read zeroes the untaken side of every branch (SPEC §5)
// through Zero<T>, and every union selection builds a fresh payload at its
// declared defaults (SPEC §4.8).
package goose

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// SerializeModule is the vendored serialize.goose entry every module with a
// packet codec imports (`import .serialize;`).
const SerializeModule = "serialize"

// WireModule is the emitted runtime file of the codec's own bit moves
// (runtime/schema_wire.goose, namespace schema_rt): serialize_bits,
// serialize_bits64 and serialize_bool written to inline at every call site,
// where each field's constant width folds them to a few instructions.
const WireModule = "schema_wire"

func init() {
	Register("wire", emitWire)
}

// wgen is one module's packet-wire emission.
type wgen struct {
	*Gen
	n int // the local-name counter, so every temp of a function is distinct
}

func (w *wgen) tmp(prefix string) string {
	w.n++
	return fmt.Sprintf("%s%d", prefix, w.n)
}

// emitWire is the part goose.Register joins: for every type and union of the
// file, in emission order, its bounds, its zero form, its writer and its
// reader.
func emitWire(g *Gen) error {
	w := &wgen{Gen: g}
	any := false
	for _, d := range ir.EmissionOrder(g.File) {
		switch d := d.(type) {
		case *ir.Struct:
			if !any {
				g.Import(SerializeModule)
				g.Import(WireModule)
				any = true
			}
			if err := w.emitStruct(d); err != nil {
				return err
			}
		case *ir.Union:
			if !any {
				g.Import(SerializeModule)
				g.Import(WireModule)
				any = true
			}
			if err := w.emitUnion(d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *wgen) emitBounds(name string, maxBits int64) {
	w.Declare(name + "MaxBits")
	w.Pf("// %sMaxBits is the longest wire path in bits, alignment padding at its worst case.\n", name)
	w.Pf("let %sMaxBits: i64 = %d;\n", name, maxBits)
	w.Declare(name + "MaxBytes")
	w.Pf("// %sMaxBytes is %sMaxBits in bytes, rounded up to the 8-byte write-buffer granularity:\n", name, name)
	w.Pf("// size write buffers from it.\n")
	w.Pf("let %sMaxBytes: i64 = %d;\n\n", name, ir.MaxBytes(maxBits))
}

func (w *wgen) emitStruct(st *ir.Struct) error {
	w.emitBounds(st.Name, ir.MaxBitsStruct(st))

	w.Declare("Zero" + st.Name)
	w.Pf("// Zero%s sets value to its zero form (SPEC §5): every field zero, a\n", st.Name)
	w.Pf("// nested type at ITS zero form — not the specified defaults, which New%s\n", st.Name)
	w.Pf("// builds. A read zeroes the untaken side of a branch with it.\n")
	w.Pf("fn Zero%s(value: %s&) {\n", st.Name, st.Name)
	w.n = 0
	for _, f := range st.Fields {
		w.emitZeroField(f, "value", "    ")
	}
	w.Pf("}\n\n")

	w.Declare("Write" + st.Name)
	w.Pf("// Write%s writes value in wire order. It returns SERIALIZE_ERROR_NONE, or the\n", st.Name)
	w.Pf("// refusal kind of the first value the wire cannot carry; the size written is\n")
	w.Pf("// serialize_bits_processed(s).\n")
	w.emitWriteEntry(st.Name)
	w.Pf("inline fn _Write%s(s: WriteStream&, l: schema_rt::WireW&, value: %s&) -> i32 {\n", st.Name, st.Name)
	w.n = 0
	if err := w.emitWriteItems(st.Items, "    "); err != nil {
		return fmt.Errorf("%s: %w", st.Name, err)
	}
	w.Pf("    return SERIALIZE_ERROR_NONE;\n}\n\n")

	w.Declare("Read" + st.Name)
	w.Pf("// Read%s reads value, validating every field. It returns false with the\n", st.Name)
	w.Pf("// reason latched on the stream (serialize_error(s)) for a wire the schema refuses.\n")
	w.Pf("fn Read%s(s: ReadStream&, value: %s&) -> bool {\n", st.Name, st.Name)
	w.n = 0
	if err := w.emitReadItems(st.Items, "    "); err != nil {
		return fmt.Errorf("%s: %w", st.Name, err)
	}
	w.Pf("    return true;\n}\n\n")
	return nil
}

func (w *wgen) emitUnion(d *ir.Union) error {
	tag := d.Name + "Type"
	w.emitBounds(d.Name, ir.MaxBitsUnion(d))

	w.Declare("Zero" + d.Name)
	w.Pf("// Zero%s sets value to its zero form (SPEC §5): the empty union (None),\n", d.Name)
	w.Pf("// every arm's storage at its zero form.\n")
	w.Pf("fn Zero%s(value: %s&) {\n    value.Type = %sNone;\n", d.Name, d.Name, tag)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		f := *v.F
		f.Name = v.Name
		w.emitZeroField(&f, "value", "    ")
	}
	w.Pf("}\n\n")

	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	w.Declare("Write" + d.Name)
	w.Pf("// Write%s writes the tag, then the selected arm's payload (SPEC §4.8). The\n", d.Name)
	w.Pf("// tag validates BEFORE it rides: an out-of-set tag writes nothing.\n")
	w.emitWriteEntry(d.Name)
	w.Pf("inline fn _Write%s(s: WriteStream&, l: schema_rt::WireW&, value: %s&) -> i32 {\n", d.Name, d.Name)
	if d.Max == 0 {
		w.Pf("    // an empty union holds only None; its degenerate tag range [0, 0] costs zero bits\n")
		w.Pf("    if value.Type != %sNone { return SERIALIZE_ERROR_VALUE_OUT_OF_RANGE; }\n", tag)
		w.Pf("    return SERIALIZE_ERROR_NONE;\n}\n\n")
	} else {
		w.Pf("    if value.Type > %sMax { return SERIALIZE_ERROR_VALUE_OUT_OF_RANGE; } // the tag validates BEFORE it rides\n", tag)
		w.Pf("    schema_rt::wire_bits(s, l, value.Type, %d);\n", bits)
		for _, v := range d.Variants {
			if v.Void() {
				continue // a payload-free arm: the tag is the whole wire
			}
			if v.Ref == nil {
				return fmt.Errorf("union %s arm %s carries no type payload, which the packet wire has no encoding for", d.Name, v.Name)
			}
			arm := ir.GoExportName(v.Name)
			w.Pf("    if value.Type == %s%s {\n", tag, arm)
			w.Pf("        return %s;\n", w.nestedWrite(v.Ref, w.TypeRef(v.Type, false), "value."+arm, "        "))
			w.Pf("    }\n")
		}
		w.Pf("    return SERIALIZE_ERROR_NONE; // None or a payload-free arm: the tag is the whole wire\n}\n\n")
	}

	w.Declare("Read" + d.Name)
	w.Pf("// Read%s reads the tag, refusing one above the arm count, then builds the\n", d.Name)
	w.Pf("// selected arm FRESH at its defaults and reads its payload into it (SPEC §4.8).\n")
	w.Pf("fn Read%s(s: ReadStream&, value: %s&) -> bool {\n", d.Name, d.Name)
	if d.Max == 0 {
		w.Pf("    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }\n")
		w.Pf("    value.Type = %sNone; // zero wire bits: only None exists\n    return true;\n}\n\n", tag)
		return nil
	}
	w.Pf("    var tag: u32 = 0;\n")
	w.Pf("    guard schema_rt::wire_bits(s, tag, %d) else { return false; }\n", bits)
	if !fills(big.NewInt(d.Max), bits) {
		w.Pf("    guard tag <= %d else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); } // not a wire-legal tag\n", d.Max)
	}
	w.Pf("    value.Type = tag as! %s;\n", tag)
	for _, v := range d.Variants {
		if v.Void() {
			continue // a payload-free arm: the tag is the whole wire
		}
		arm := ir.GoExportName(v.Name)
		typ := w.TypeRef(v.Type, false)
		w.Pf("    if value.Type == %s%s {\n", tag, arm)
		w.Pf("        value.%s = %s { .. }; // a fresh payload on every selection\n", arm, typ)
		w.Pf("        return Read%s(s, value.%s);\n", typ, arm)
		w.Pf("    }\n")
	}
	w.Pf("    return true; // None or a payload-free arm: the tag is the whole wire\n}\n\n")
	return nil
}

// fills reports whether a span covers every value `bits` wire bits carry,
// which makes a headroom refusal vacuous.
func fills(span *big.Int, bits int64) bool {
	full := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return span.Cmp(full) == 0
}

// u64Lit renders v modulo 2^64 as a u64 decimal literal: the two's
// complement of a negative bound, the domain the offset arithmetic runs in.
func u64Lit(v *big.Int) string {
	m := new(big.Int).And(v, mask64)
	return m.String()
}

// storageExtremes is an integer storage's own range.
func storageExtremes(signed bool, width int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(width-1)), one)
		return new(big.Int).Neg(new(big.Int).Lsh(one, uint(width-1))), hi
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(width)), one)
}

// storageWidth is the Goose width an integer of the declared width is held
// at (THE NUMBER MODEL).
func storageWidth(width int) int {
	switch {
	case width <= 8:
		return 8
	case width <= 16:
		return 16
	case width <= 32:
		return 32
	case width <= 64:
		return 64
	}
	return 128
}

// ---- the zero form ----------------------------------------------------------

func (w *wgen) emitZeroField(f *ir.Field, base, ind string) {
	name := base + "." + ir.GoExportName(f.Name)
	t := f.Type
	switch {
	case t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString:
		if f.Array != ir.ArrayNone {
			i := w.tmp("ix")
			w.Pf("%sfor %s in %d {\n%s    for %s in %d { %s[%s][%s] = 0; }\n%s    %sLength[%s] = 0;\n%s}\n",
				ind, i, f.ArrayBound, ind, "j"+i, t.Size, name, i, "j"+i, ind, name, i, ind)
			if f.Array == ir.ArrayCounted {
				w.Pf("%s%sCount = 0;\n", ind, name)
			}
			return
		}
		i := w.tmp("ix")
		w.Pf("%sfor %s in %d { %s[%s] = 0; }\n%s%sLength = 0;\n", ind, i, t.Size, name, i, ind, name)
	case f.Array != ir.ArrayNone:
		i := w.tmp("ix")
		w.Pf("%sfor %s in %d {\n", ind, i, f.ArrayBound)
		w.emitZeroScalar(f, name+"["+i+"]", ind+"    ")
		w.Pf("%s}\n", ind)
		if f.Array == ir.ArrayCounted {
			w.Pf("%s%sCount = 0;\n", ind, name)
		}
	default:
		w.emitZeroScalar(f, name, ind)
	}
}

func (w *wgen) emitZeroScalar(f *ir.Field, expr, ind string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		w.Pf("%s%s = false;\n", ind, expr)
	case ir.TFloat32, ir.TFloat64:
		w.Pf("%s%s = 0.0;\n", ind, expr)
	case ir.TInt, ir.TFixed:
		if t.Width > 64 {
			w.Pf("%s%s = %s::Int128 { lo: 0, hi: 0 };\n", ind, expr, RuntimeNamespace)
			return
		}
		w.Pf("%s%s = 0;\n", ind, expr)
	case ir.TBits:
		w.Pf("%s%s = 0;\n", ind, expr)
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			w.Pf("%sZero%s(%s);\n", ind, w.TypeRef(t.Name, false), expr)
		default:
			w.Pf("%s%s = 0;\n", ind, expr)
		}
	}
}

// ---- items -------------------------------------------------------------------

func condExpr(b *ir.Branch) string {
	c := "value." + ir.GoExportName(b.Cond)
	if b.Neg {
		return "!" + c
	}
	return c
}

func (w *wgen) emitWriteItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch it := item.(type) {
		case *ir.FieldItem:
			if err := w.emitWriteField(it.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			w.Pf("%s%s // const(%s, %d) (SPEC §4.3)\n", ind, bitsCall(u64Lit(it.Value), it.Bits), it.Value.String(), it.Bits)
		case *ir.ReservedItem:
			w.Pf("%s%s // reserved(%d): zeros on the wire\n", ind, bitsCall("0", it.Bits), it.Bits)
		case *ir.AlignItem:
			w.streamCall(ind, "%sserialize_align(s);\n", ind)
		case *ir.Branch:
			w.Pf("%sif %s {\n", ind, condExpr(it))
			if err := w.emitWriteItems(it.Then, ind+"    "); err != nil {
				return err
			}
			if it.Else != nil {
				w.Pf("%s} else {\n", ind)
				if err := w.emitWriteItems(it.Else, ind+"    "); err != nil {
					return err
				}
			}
			w.Pf("%s}\n", ind)
		}
	}
	return nil
}

func (w *wgen) emitReadItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch it := item.(type) {
		case *ir.FieldItem:
			if err := w.emitReadField(it.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			v := w.readRaw(it.Bits, ind)
			w.Pf("%sguard %s == %s else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); } // const(%s, %d): a read refuses any other value\n",
				ind, v, u64Lit(it.Value), it.Value.String(), it.Bits)
		case *ir.ReservedItem:
			v := w.readRaw(it.Bits, ind)
			w.Pf("%sguard %s == 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); } // reserved(%d): a read refuses nonzero\n", ind, v, it.Bits)
		case *ir.AlignItem:
			w.Pf("%sguard serialize_align(s) else { return false; } // refuses nonzero padding\n", ind)
		case *ir.Branch:
			w.Pf("%sif %s {\n", ind, condExpr(it))
			if err := w.emitReadItems(it.Then, ind+"    "); err != nil {
				return err
			}
			// the untaken side reads as its zero form (SPEC §5)
			w.emitZeroItems(it.Else, ind+"    ")
			w.Pf("%s} else {\n", ind)
			if err := w.emitReadItems(it.Else, ind+"    "); err != nil {
				return err
			}
			w.emitZeroItems(it.Then, ind+"    ")
			w.Pf("%s}\n", ind)
		}
	}
	return nil
}

func (w *wgen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch it := item.(type) {
		case *ir.FieldItem:
			w.emitZeroField(it.F, "value", ind)
		case *ir.Branch:
			w.emitZeroItems(it.Then, ind)
			w.emitZeroItems(it.Else, ind)
		}
	}
}

// bitsCall is the write of a u64-valued expression in `bits` wire bits: one
// call of 32 bits or fewer, else wire_bits64, whose low 32 bits go first
// (STANDARD.md, bits). A zero-bit write is nothing.
func bitsCall(expr string, bits int64) string {
	if bits == 0 {
		return ""
	}
	if bits <= 32 {
		return fmt.Sprintf("schema_rt::wire_bits(s, l, %s, %d);", narrow32(expr), bits)
	}
	return fmt.Sprintf("schema_rt::wire_bits64(s, l, %s, %d);", expr, bits)
}

// narrow32 spells a value known to fit 32 bits as a u32 argument: a literal
// adapts to the parameter, anything else narrows with `as!` (THE NUMBER
// MODEL: never checked, and the value fits by construction).
func narrow32(expr string) string {
	if _, err := strconv.ParseUint(expr, 10, 64); err == nil {
		return expr
	}
	return "(" + expr + ") as! u32"
}

// readRaw reads `bits` wire bits into a fresh u64 local and returns its name;
// a failed read returns false with the runtime's reason latched.
func (w *wgen) readRaw(bits int64, ind string) string {
	v := w.tmp("raw")
	if bits <= 32 {
		n := w.tmp("narrow")
		w.Pf("%svar %s: u32 = 0;\n", ind, n)
		w.Pf("%sguard schema_rt::wire_bits(s, %s, %d) else { return false; }\n", ind, n, bits)
		w.Pf("%slet %s: u64 = %s;\n", ind, v, n)
		return v
	}
	w.Pf("%svar %s: u64 = 0;\n", ind, v)
	w.Pf("%sguard schema_rt::wire_bits64(s, %s, %d) else { return false; }\n", ind, v, bits)
	return v
}

// intGoose is the Goose spelling of an integer storage.
func intGoose(signed bool, width int) string {
	return IntType(signed, width)
}

// floatLit32 renders a float32 constant as a literal that converts back to
// exactly its bits: the folded compressed-float constants ride the wire.
func floatLit32(v float32) string {
	if v == 0 {
		return "0.0"
	}
	s := strconv.FormatFloat(float64(v), 'g', -1, 32)
	s = strings.Replace(s, "e+", "e", 1)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	if math.Signbit(float64(v)) {
		return "(" + s + ")"
	}
	return s
}

// ---- the writer's packer state in locals --------------------------------------

// Every Write<T> is a thin entry over an `inline fn _Write<T>` that carries the
// packer's state (scratch, scratch_bits, bits_written, word_index) in a
// schema_rt::WireW the entry owns: the bit moves update locals, which the C
// compiler keeps in registers, rather than the stream's fields through a
// pointer that every byte store may alias (runtime/schema_wire.goose). The
// entry puts the state back into the stream before it returns, and every
// runtime call that takes the stream gets it put back first and read back
// after (streamCall), so the stream is exactly what serialize.goose's own
// entries would have left.
func (w *wgen) emitWriteEntry(name string) {
	w.Pf("fn Write%s(s: WriteStream&, value: %s&) -> i32 {\n", name, name)
	w.Pf("    var l = schema_rt::wire_open(s);\n")
	w.Pf("    let refusal = _Write%s(s, l, value);\n", name)
	w.Pf("    schema_rt::wire_close(s, l);\n")
	w.Pf("    return refusal;\n}\n\n")
}

// streamCall emits a runtime call that takes the stream, the packer state put
// back into it first and read back after.
func (w *wgen) streamCall(ind, format string, args ...any) {
	w.Pf("%sschema_rt::wire_close(s, l);\n", ind)
	w.Pf(format, args...)
	w.Pf("%sschema_rt::wire_reopen(s, l);\n", ind)
}

// inlineWriteLimit bounds the code a nested type's writer may add at each
// call site: a type whose flattened write is larger is called through its
// entry, its state put back around the call, so nesting never multiplies
// code without bound.
const inlineWriteLimit = 256

// nestedWrite is the expression that writes a nested type or union: its
// inline body over this writer's state when it is small, else its entry.
func (w *wgen) nestedWrite(ref any, typ, expr, ind string) string {
	if writeCost(ref, map[any]int{}) <= inlineWriteLimit {
		return fmt.Sprintf("_Write%s(s, l, %s)", typ, expr)
	}
	w.Pf("%sschema_rt::wire_close(s, l);\n", ind)
	t := w.tmp("nested")
	w.Pf("%slet %s = Write%s(s, %s);\n", ind, t, typ, expr)
	w.Pf("%sschema_rt::wire_reopen(s, l);\n", ind)
	return t
}

// writeCost is the flattened size of a type's write, in fields: a nested type
// small enough to inline counts its own cost, a larger one counts one call.
func writeCost(ref any, memo map[any]int) int {
	if c, ok := memo[ref]; ok {
		return c
	}
	memo[ref] = inlineWriteLimit + 1 // a cycle never inlines
	nested := func(t ir.FieldType) int {
		if t.Kind == ir.TNamed {
			switch r := t.Ref.(type) {
			case *ir.Struct, *ir.Union:
				if c := writeCost(r, memo); c <= inlineWriteLimit {
					return c
				}
			}
		}
		return 1
	}
	var items func([]ir.Item) int
	items = func(list []ir.Item) int {
		n := 0
		for _, item := range list {
			switch it := item.(type) {
			case *ir.FieldItem:
				n += nested(it.F.Type)
			case *ir.Branch:
				n += 1 + items(it.Then) + items(it.Else)
			default:
				n++
			}
		}
		return n
	}
	c := 1
	switch r := ref.(type) {
	case *ir.Struct:
		c = items(r.Items)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.Ref == nil {
				c++
			} else if a := writeCost(v.Ref, memo); a <= inlineWriteLimit {
				c += a
			} else {
				c++
			}
		}
	}
	memo[ref] = c
	return c
}
