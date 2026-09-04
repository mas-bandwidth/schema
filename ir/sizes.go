// Checked size arithmetic: the ONE place every size, stride, offset and
// MaxBytes this package derives is added, multiplied and rounded up.
//
// Individual bounds are legal in isolation and their PRODUCT is not: three
// nested [2147483647] arrays are three legal bounds (SPEC §4.3 caps a bound at
// int32, and nothing caps what they multiply to) whose widths pass int64.
// Unguarded arithmetic wrapped there, and a wrapped number reaches a generated
// file as a negative stride, a negative static_assert size, or a
// plausible-looking positive buffer bound that under-states the truth by
// orders of magnitude.
//
// Two properties close that class. The helpers below SATURATE rather than
// wrap, so no derived size is ever negative or smaller than the number it
// stands for; and [CheckSizes] refuses a unit whose numbers pass the cap,
// naming the field and the product, before any backend is handed the IR
// (SPEC §4.6, docs/SPEC-TABLES.md §11).
package ir

import (
	"fmt"
	"math/big"
	"sort"
)

// MaxSizeBytes is the cap on every size the compiler derives: a record's
// storage, an array's whole storage, a block form's extent, a MaxBytes buffer
// bound. A terabyte is past anything a target can hold and past the largest
// node body the wire can frame (a node's length is a u32, docs/SPEC-TABLES.md
// §3.1), so no legal schema meets it; what it buys is that every number the
// compiler emits is small enough that the arithmetic reaching it cannot wrap.
const MaxSizeBytes = int64(1) << 40

// MaxWireBits is the same cap in the wire's unit: the bits that fit
// MaxSizeBytes bytes. A width past it could not be advertised as a buffer
// bound anyway, because [MaxBytes] of it would pass the byte cap.
const MaxWireBits = MaxSizeBytes * 8

// sizeCeiling is where the checked helpers saturate. It is a power of two far
// above [MaxSizeBytes] and far below math.MaxInt64, which gives three
// properties the arithmetic below depends on: a saturated value can be added
// and multiplied again without wrapping, rounding one up to any alignment this
// package uses returns it unchanged (it is a multiple of every power of two up
// to itself), and it is unmistakably past the cap, so a saturated number is
// always refused rather than emitted.
const sizeCeiling = int64(1) << 62

// addSize is a + b for two non-negative sizes, saturating at [sizeCeiling].
func addSize(a, b int64) int64 {
	if a > sizeCeiling-b {
		return sizeCeiling
	}
	return a + b
}

// mulSize is a * b for two non-negative sizes, saturating at [sizeCeiling].
func mulSize(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > sizeCeiling/b {
		return sizeCeiling
	}
	return a * b
}

// alignUp rounds v up to a multiple of a, saturating at [sizeCeiling]. It is
// the one rounding step in the layout model: every record size, every field
// offset and every block array start goes through it.
func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	if r := v % a; r != 0 {
		return addSize(v, a-r)
	}
	return v
}

// SizeError is one derived size the compiler refuses: which declaration and
// field it belongs to, and the arithmetic that passed the cap.
type SizeError struct {
	Owner     string // the declaration the number belongs to
	OwnerKind string // "type" or "table"
	Field     string // the field at fault, or "" when the record total is
	Detail    string // the arithmetic, spelled out against its cap
}

func (e SizeError) Error() string {
	where := e.OwnerKind + " " + e.Owner
	if e.Field != "" {
		where += ": field " + e.Field
	}
	return where + " " + e.Detail
}

// productDetail spells one product out exactly. The operands are int64 and
// their product is not, so the number is formed in big.Int: this is the
// message for an already-refused computation, and a diagnostic that says
// "past the cap" without saying by how much sends its reader guessing.
func productDetail(what string, count, each int64, unit string, limit int64) string {
	exact := new(big.Int).Mul(big.NewInt(count), big.NewInt(each))
	return fmt.Sprintf("is %d %s x %d %s each = %s %s, past the cap of %d %s on a derived size: every bound here is legal on its own and their product is not (SPEC §4.6, docs/SPEC-TABLES.md §11)",
		count, what, each, unit, exact, unit, limit, unit)
}

// totalDetail spells a record total out. Every term of it is inside the cap
// (a record whose parts are not is reported by those parts), so the sum is
// exact in int64.
func totalDetail(what string, total int64, unit string, limit int64) string {
	return fmt.Sprintf("%s is %d %s, past the cap of %d %s on a derived size (SPEC §4.6, docs/SPEC-TABLES.md §11)",
		what, total, unit, limit, unit)
}

// CheckSizes recomputes every size this package derives for a unit, through
// the same helpers the backends call, and returns one error per number that
// passes a cap. The checker runs it and refuses the unit, so no generated file
// can carry a size the arithmetic could not represent.
//
// A number is reported ONCE, at the declaration it belongs to: a field whose
// element type is itself past the cap is skipped, because that element's own
// declaration carries the diagnostic and reporting a product of an already
// saturated operand would name a number that stands for nothing.
func CheckSizes(u *Unit) []SizeError {
	var out []SizeError
	for _, name := range sizeCheckOrder(u) {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		out = append(out, checkRecordSizes(u, st)...)
	}
	if b := Blocks(u); b != nil {
		for _, bl := range b.Tables {
			out = append(out, checkBlockSizes(bl)...)
		}
	}
	return out
}

// sizeCheckOrder is every record of a unit, tables and types alike, in one
// sorted order so the diagnostics do not shuffle run to run.
func sizeCheckOrder(u *Unit) []string {
	names := make([]string, 0, len(u.Structs)+len(u.Tables))
	for name := range u.Structs {
		names = append(names, name)
	}
	for name := range u.Tables {
		if _, dup := u.Structs[name]; !dup {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// checkRecordSizes covers one record: each field's wire width and storage, then
// the record's own totals when no field of it was already refused.
func checkRecordSizes(u *Unit, st *Struct) []SizeError {
	kind := "type"
	if st.IsTable {
		kind = "table"
	}
	var out []SizeError
	poisoned := false
	for _, f := range st.Fields {
		elemBits := maxBitsScalar(f)
		elemBytes := fieldElemBytes(u, f)
		if elemBits > MaxWireBits || elemBytes > MaxSizeBytes {
			// the element's own declaration carries the diagnostic
			poisoned = true
			continue
		}
		if f.Array == ArrayNone {
			continue
		}
		if MaxBitsField(f) > MaxWireBits {
			out = append(out, SizeError{Owner: st.Name, OwnerKind: kind, Field: f.Name,
				Detail: productDetail("elements", f.ArrayBound, elemBits, "bits", MaxWireBits)})
			poisoned = true
			continue
		}
		if mulSize(f.ArrayBound, elemBytes) > MaxSizeBytes {
			out = append(out, SizeError{Owner: st.Name, OwnerKind: kind, Field: f.Name,
				Detail: productDetail("elements", f.ArrayBound, elemBytes, "bytes", MaxSizeBytes)})
			poisoned = true
		}
	}
	if poisoned {
		return out
	}
	if bits := MaxBitsStruct(st); bits > MaxWireBits {
		out = append(out, SizeError{Owner: st.Name, OwnerKind: kind,
			Detail: totalDetail("its wire width", bits, "bits", MaxWireBits)})
	}
	if size := layoutRecord(u, st).Size; size > MaxSizeBytes {
		out = append(out, SizeError{Owner: st.Name, OwnerKind: kind,
			Detail: totalDetail("its storage", size, "bytes", MaxSizeBytes)})
	}
	return out
}

// fieldElemBytes is ONE value of a field's declared type in storage: the array
// element, or the scalar itself. A string, a bytes and a map carry companions
// rather than elements, so their whole storage is the value.
func fieldElemBytes(u *Unit, f *Field) int64 {
	var total int64
	for _, p := range fieldPieces(u, f, false) {
		total = addSize(total, p.size)
	}
	if f.Array == ArrayNone {
		return total
	}
	return elementPiece(u, f).size
}

// checkBlockSizes covers one block form: each out-of-line array at its declared
// maximum, then the extent those maxima sum to (docs/SPEC-TABLES.md §19.1).
func checkBlockSizes(bl *BlockLayout) []SizeError {
	var out []SizeError
	poisoned := false
	for _, a := range bl.Arrays {
		if a.Stride > MaxSizeBytes {
			poisoned = true
			continue
		}
		if mulSize(a.Max, a.Stride) > MaxSizeBytes {
			out = append(out, SizeError{Owner: bl.Table.Name, OwnerKind: "table", Field: a.Field.Name,
				Detail: productDetail("rows", a.Max, a.Stride, "bytes", MaxSizeBytes)})
			poisoned = true
		}
	}
	if poisoned {
		return out
	}
	if bl.MaxBytes > MaxSizeBytes {
		out = append(out, SizeError{Owner: bl.Table.Name, OwnerKind: "table",
			Detail: totalDetail("its block form's extent", bl.MaxBytes, "bytes", MaxSizeBytes)})
	}
	return out
}
