package tabletext

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// InvalidValue is a writer's refusal of a value outside its declaration
// (docs/SPEC-TABLES.md §5, §16.3, the `invalid_value` row of §6.5): a value off
// its min or max, an integer too wide for its storage or for its bits(n), an
// enum ordinal or a flags bit that names no variant, a deprecated variant, a
// union tag past its arms, a count or length past its bound, or ill-formed
// text. A non-finite float is the writers' own refusal, non_finite_float. Every writer asks CheckValue before the first byte
// it writes, so what a writer accepts is exactly what a read accepts: a writer
// never clamps and never truncates.
type InvalidValue struct {
	Field  string // Owner.field
	Detail string
}

func (e *InvalidValue) Error() string {
	return "invalid_value: " + e.Field + ": " + e.Detail
}

// CheckValue walks a value as the writers walk it (absent optionals, elided
// guards and deprecated fields are not written, so they are not asked) and
// answers the first value outside its declaration as an *InvalidValue. It is
// the check of the writers that carry a flags mask by its names or at its
// declared W bits, the text form and the message form, so a mask bit that
// names no variant (one a newer writer appended) refuses there rather than
// being dropped.
func (m *Model) CheckValue(inst *Instance) error {
	return m.checkInstance(inst, &checkWalk{seen: map[*Instance]bool{}})
}

// CheckFileValue is CheckValue for the file forms, variable and fixed, which
// carry a flags mask whole as a u64 (docs/SPEC-TABLES.md §3, §3.4): a mask
// bit above the declared variants is kept, as the read keeps it (§4, A TABLE
// LOAD DOES NOT JUDGE A MASK AGAINST A RANGE), so a file an older build read
// writes back byte for byte. A FILE writer also refuses a zero byte or unit
// (docs/SPEC.md §5.1 row 8): its reader calls that text_ill_formed, so a
// writer never produces bytes its own reader refuses.
func (m *Model) CheckFileValue(inst *Instance) error {
	return m.checkInstance(inst, &checkWalk{seen: map[*Instance]bool{}, wholeMasks: true, binaryText: true})
}

// CheckBinaryValue is CheckValue for the binary writers that do not carry a
// file's flags mask, which is the message form: a zero byte or unit is
// ill-formed text there too (docs/SPEC.md §5.1 row 8), while the TEXT writer
// alone spells it `\u0000`.
func (m *Model) CheckBinaryValue(inst *Instance) error {
	return m.checkInstance(inst, &checkWalk{seen: map[*Instance]bool{}, binaryText: true})
}

// checkWalk is one CheckValue walk: the instances already asked, whether the
// writer carries a flags mask whole, and whether a zero byte or unit is
// ill-formed text for this writer.
type checkWalk struct {
	seen       map[*Instance]bool
	wholeMasks bool
	binaryText bool
}

func (m *Model) checkInstance(inst *Instance, seen *checkWalk) error {
	if inst == nil || seen.seen[inst] {
		return nil
	}
	seen.seen[inst] = true
	guards := Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if fv.Def.Deprecated {
			continue
		}
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if err := m.checkField(inst.Def.Name, fv, seen); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) checkField(owner string, fv *Field, seen *checkWalk) error {
	f := fv.Def
	name := f.Name
	if owner != "" {
		name = owner + "." + f.Name
	}
	if f.Type.Optional && !fv.Present {
		return nil
	}
	bad := func(format string, a ...any) error {
		return &InvalidValue{Field: name, Detail: fmt.Sprintf(format, a...)}
	}
	if f.Type.Blob() {
		if b := fv.Cell.Blob; b != nil && f.Type.Kind == ir.TString && !utf8.Valid(b.Data) {
			return bad("ill-formed UTF-8 text")
		}
		return nil
	}
	if f.IsMap() {
		for i := range fv.Entries {
			if err := m.checkInstance(fv.Entries[i].Tab, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if f.Array != ir.ArrayNone {
		cells := fv.Elems
		if f.CountedOnWire() {
			if fv.Count < 0 || fv.Count > len(cells) || (f.Array == ir.ArrayCounted && int64(fv.Count) > f.ArrayBound) {
				return bad("count %d past its bound", fv.Count)
			}
			if f.Array == ir.ArrayCounted && int64(fv.Count) < f.ArrayMin {
				return bad("count %d under its minimum %d", fv.Count, f.ArrayMin)
			}
			cells = cells[:fv.Count]
		}
		for i := range cells {
			if err := m.checkCell(name, &cells[i], f, seen, bad); err != nil {
				return err
			}
		}
		return nil
	}
	return m.checkCell(name, &fv.Cell, f, seen, bad)
}

func (m *Model) checkCell(name string, cell *Cell, f *ir.Field, seen *checkWalk, bad func(string, ...any) error) error {
	if un := UnionOf(f); un != nil {
		if cell.U == 0 {
			return nil
		}
		if cell.U > uint64(len(un.Variants)) {
			return bad("union %s tag %d names no arm", un.Name, cell.U)
		}
		arm := un.Variants[cell.U-1]
		switch {
		case arm.Void():
			return nil
		case arm.Body():
			return m.checkInstance(cell.Tab, seen)
		case cell.Arm != nil:
			return m.checkField(name, cell.Arm, seen)
		}
		return nil
	}
	if st := StructOf(f); st != nil {
		if f.Type.Pointer {
			return m.checkInstance(cell.Node, seen)
		}
		return m.checkInstance(cell.Tab, seen)
	}
	if e := EnumOf(f); e != nil {
		if EnumName(e, int64(cell.U)) == "" {
			return bad("enum %s ordinal %d names no variant", e.Name, cell.U)
		}
		if VariantDeprecated(e, int64(cell.U)) {
			return bad("enum %s ordinal %d names a deprecated variant", e.Name, cell.U)
		}
		return nil
	}
	if fl := FlagsOf(f); fl != nil {
		if n := len(fl.Variants); !seen.wholeMasks && n < 64 && cell.U>>uint(n) != 0 {
			return bad("flags %s bits %#x name no variant", fl.Name, cell.U&^(uint64(1)<<uint(n)-1))
		}
		return nil
	}
	if ir.TextListPlain(f) || f.IsTextList() {
		return m.checkInstance(cell.Tab, seen)
	}
	switch f.Type.Kind {
	case ir.TString:
		if !utf8.Valid(cell.Str) {
			return bad("ill-formed UTF-8 text")
		}
		if seen.binaryText && bytes.IndexByte(cell.Str, 0) >= 0 {
			return bad("ill-formed UTF-8 text (a zero byte)")
		}
		if !f.Type.Unbounded && f.Type.Size > 0 && int64(len(cell.Str)) > f.Type.Size {
			return bad("length %d past its bound %d", len(cell.Str), f.Type.Size)
		}
		return nil
	case ir.TWString:
		if !wellFormedUTF16(cell.Units) {
			return bad("ill-formed UTF-16 text (a lone surrogate)")
		}
		if seen.binaryText {
			for _, u := range cell.Units {
				if u == 0 {
					return bad("ill-formed UTF-16 text (a zero unit)")
				}
			}
		}
		if f.Type.Size > 0 && int64(len(cell.Units)) > f.Type.Size {
			return bad("length %d past its bound %d", len(cell.Units), f.Type.Size)
		}
		return nil
	case ir.TBytes:
		if f.Type.Size > 0 && int64(len(cell.Str)) > f.Type.Size {
			return bad("length %d past its bound %d", len(cell.Str), f.Type.Size)
		}
		return nil
	}
	kind := ir.TableScalarKind(f)
	switch kind {
	case ir.TableKindBool, ir.TableKindTimestamp:
		return nil
	case ir.TableKindF32, ir.TableKindF64:
		if math.IsNaN(cell.F) || math.IsInf(cell.F, 0) {
			// every writer refuses a non-finite float by its own name,
			// non_finite_float (§3.3), at the float it writes
			return nil
		}
		if kind == ir.TableKindF32 && math.Abs(cell.F) > math.MaxFloat32 {
			return bad("%v past float32", cell.F)
		}
		if f.HasFloatRange && (cell.F < f.FMin || cell.F > f.FMax) {
			return bad("%v off its range [%v, %v]", cell.F, f.FMin, f.FMax)
		}
		return nil
	}
	if kind < 0 {
		return nil
	}
	signed := ir.TableKindSigned(kind)
	var raw big.Int
	switch {
	case ir.TableKindWide(kind):
		raw.Set(WideValue(cell))
	case signed:
		raw.SetInt64(int64(cell.U))
	default:
		raw.SetUint64(cell.U)
	}
	bits := 8 * StorageBytes(f)
	if f.Type.Kind == ir.TBits || f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TInt {
		if f.Type.Width > 0 {
			bits = f.Type.Width
		}
	}
	if ir.TableKindWide(kind) && f.Type.Kind != ir.TFixed && f.Type.Width == 0 {
		bits = 8 * ir.TableKindWidth(kind)
	}
	if !fitsBits(&raw, bits, signed) {
		return bad("%s does not fit in %d bits", raw.String(), bits)
	}
	if !ir.TableKindWide(kind) && bits < 64 && signed {
		// the storage member a narrow signed integer is held in is its own
		// width, sign-extended: U past it is another value
		lo := int64(-1) << uint(bits-1)
		if v := int64(cell.U); v < lo || v > -lo-1 {
			return bad("%d does not fit in %d bits", v, bits)
		}
	}
	if lo, hi, ok := ir.TableRawRange(f); ok && (raw.Cmp(lo) < 0 || raw.Cmp(hi) > 0) {
		return bad("%s off its range [%s, %s]", raw.String(), lo.String(), hi.String())
	}
	return nil
}

func fitsBits(v *big.Int, bits int, signed bool) bool {
	if bits <= 0 {
		return true
	}
	if signed {
		lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)))
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
		return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
	}
	return v.Sign() >= 0 && v.BitLen() <= bits
}

// wellFormedUTF16 is whether units pair every surrogate: a lead followed by a
// trail, and no trail on its own.
func wellFormedUTF16(units []uint16) bool {
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				return false
			}
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return false
		}
	}
	return true
}
