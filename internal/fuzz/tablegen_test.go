package fuzz_test

// tablegen_test.go is the seeded generator half of the random-schema
// differential harness (card schema-go-fuzz-random-table-differential). It
// builds VALID table units (fixed Root) from a math/rand seed and writes JSON
// text for any value in range, with the bias to both ends the card names.
//
// The generator only builds constructs the checker accepts: one-sided ranges,
// ranges excluding zero without a default, arrays of bytes/timestamp,
// ?varstring, ? over a variable closure, attributes on a map and text lists in
// fixed tables are all in the campaign's refusal census and never produced
// here.

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
)

// genKind is the algebraic type of one generated field.
type genKind int

const (
	gInt genKind = iota
	gUint
	gFloat32
	gFloat64
	gBool
	gEnum
	gString
	gBytes
	gTimestamp
	gScalarArray
	gOptScalar
	gNested
)

// intRange mode for signed/unsigned scalars.
type intMode int

const (
	mNone intMode = iota // no bounds declared
	mBothZero            // both ends hold zero
	mExtreme             // both ends at the implied extremes
	mExclDefault         // exclude zero with an explicit in-range default
)

type genField struct {
	Name string
	Kind genKind
	Bits int // 8/16/32/64 for int/uint

	IMode intMode
	Imin  int64
	Imax  int64
	IDef  int64

	EnumVar string // variable name of the enum type
	EnumVal string // a valid member

	StrN int

	Elem *genField // element for scalar array / optional / nested
	MaxN int        // array max length ([..N])

	NestedName string
}

type genUnit struct {
	Enums  []string // enum type names (0-2)
	Fields []*genField
	Root   *genField // container; fields listed in Fields
}

func pickIntMode(r *rand.Rand) intMode {
	switch r.Intn(4) {
	case 0:
		return mNone
	case 1:
		return mBothZero
	case 2:
		return mExtreme
	default:
		return mExclDefault
	}
}

func intBounds(bits int, mode intMode) (min, max, def int64, hasDef bool) {
	switch bits {
	case 8:
		min, max = math.MinInt8, math.MaxInt8
	case 16:
		min, max = math.MinInt16, math.MaxInt16
	case 32:
		min, max = math.MinInt32, math.MaxInt32
	default:
		min, max = math.MinInt64, math.MaxInt64
	}
	switch mode {
	case mNone:
		return min, max, 0, false
	case mBothZero:
		return 0, 0, 0, false
	case mExtreme:
		return min, max, 0, false
	case mExclDefault:
		// exclude zero; a default in range, explicit
		if bits <= 16 {
			return 1, max, 1, true
		}
		return 1, max, 1, true
	}
	return min, max, 0, false
}

func uintBounds(bits int, mode intMode) (min, max uint64, def uint64, hasDef bool) {
	var span uint64
	switch bits {
	case 8:
		span = math.MaxUint8
	case 16:
		span = math.MaxUint16
	case 32:
		span = math.MaxUint32
	default:
		span = math.MaxUint64
	}
	switch mode {
	case mNone:
		return 0, span, 0, false
	case mBothZero:
		return 0, 0, 0, false
	case mExtreme:
		return 0, span, 0, false
	case mExclDefault:
		return 1, span, 1, true
	}
	return 0, span, 0, false
}

var enumPool = []string{"Red", "Green", "Blue"}

// randomField builds one field.
func randomField(r *rand.Rand, enumVars []string) *genField {
	k := r.Intn(12)
	f := &genField{Name: fmt.Sprintf("f%d", r.Intn(100000))}
	switch {
	case k < 2:
		f.Kind = gInt
		f.Bits = []int{8, 16, 32, 64}[r.Intn(4)]
		f.IMode = pickIntMode(r)
		f.Imin, f.Imax, f.IDef, _ = intBounds(f.Bits, f.IMode)
	case k < 3:
		f.Kind = gUint
		f.Bits = []int{8, 16, 32, 64}[r.Intn(4)]
		f.IMode = pickIntMode(r)
	case k == 3:
		f.Kind = gFloat32
	case k == 4:
		f.Kind = gFloat64
	case k == 5:
		f.Kind = gBool
	case k == 6:
		f.Kind = gEnum
		f.EnumVar = enumVars[r.Intn(len(enumVars))]
		f.EnumVal = enumPool[r.Intn(len(enumPool))]
	case k == 7:
		f.Kind = gString
		f.StrN = 2 + r.Intn(16)
	case k == 8:
		f.Kind = gBytes
		f.StrN = 2 + r.Intn(8)
	case k == 9:
		f.Kind = gTimestamp
	case k == 10:
		f.Kind = gScalarArray
		f.Elem = randomScalar(r, enumVars)
		f.MaxN = 1 + r.Intn(4)
	case k == 11:
		f.Kind = gOptScalar
		f.Elem = randomScalar(r, enumVars)
	}
	return f
}

func randomScalar(r *rand.Rand, enumVars []string) *genField {
	f := &genField{Name: "e"}
	switch r.Intn(7) {
	case 0:
		f.Kind = gInt
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
	case 1:
		f.Kind = gUint
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
	case 2:
		f.Kind = gFloat32
	case 3:
		f.Kind = gFloat64
	case 4:
		f.Kind = gBool
	case 5:
		f.Kind = gEnum
		f.EnumVar = enumVars[r.Intn(len(enumVars))]
		f.EnumVal = enumPool[r.Intn(len(enumPool))]
	default:
		f.Kind = gString
		f.StrN = 2 + r.Intn(8)
	}
	return f
}

func typeName(f *genField, enumVars []string) string {
	switch f.Kind {
	case gInt:
		return fmt.Sprintf("int%d", f.Bits)
	case gUint:
		return fmt.Sprintf("uint%d", f.Bits)
	case gFloat32:
		return "float32"
	case gFloat64:
		return "float64"
	case gBool:
		return "bool"
	case gEnum:
		return f.EnumVar
	case gString:
		return fmt.Sprintf("string(%d)", f.StrN)
	case gBytes:
		return fmt.Sprintf("bytes(%d)", f.StrN)
	case gTimestamp:
		return "timestamp"
	case gScalarArray:
		return fmt.Sprintf("[..%d]%s", f.MaxN, typeName(f.Elem, enumVars))
	case gOptScalar:
		return "?" + typeName(f.Elem, enumVars)
	case gNested:
		return f.NestedName
	}
	return "bool"
}

func fieldDecl(f *genField, enumVars []string) string {
	t := typeName(f, enumVars)
	switch f.Kind {
	case gInt:
		switch f.IMode {
		case mBothZero:
			return fmt.Sprintf("%s int%d = 0", f.Name, f.Bits)
		case mExclDefault:
			return fmt.Sprintf("%s int%d = %d | min = %d, max = %d", f.Name, f.Bits, f.IDef, f.Imin, f.Imax)
		case mExtreme:
			return fmt.Sprintf("%s int%d | min = %d, max = %d", f.Name, f.Bits, f.Imin, f.Imax)
		default:
			return fmt.Sprintf("%s int%d", f.Name, f.Bits)
		}
	case gUint:
		min, max, def, _ := uintBounds(f.Bits, f.IMode)
		switch f.IMode {
		case mBothZero:
			return fmt.Sprintf("%s uint%d = 0", f.Name, f.Bits)
		case mExclDefault:
			return fmt.Sprintf("%s uint%d = %d | min = %d, max = %d", f.Name, f.Bits, def, min, max)
		case mExtreme:
			return fmt.Sprintf("%s uint%d | min = %d, max = %d", f.Name, f.Bits, min, max)
		default:
			return fmt.Sprintf("%s uint%d", f.Name, f.Bits)
		}
	}
	return fmt.Sprintf("%s %s", f.Name, t)
}

// randomSchema produces a valid schema source for a fixed Root, the unit
// model, and the enum variable names.
func randomSchema(r *rand.Rand) (string, *genUnit) {
	u := &genUnit{}
	ne := r.Intn(3)
	for i := 0; i < ne; i++ {
		u.Enums = append(u.Enums, fmt.Sprintf("Enum%d", i))
	}
	if len(u.Enums) == 0 {
		u.Enums = append(u.Enums, "Enum0")
	}
	var b strings.Builder
	b.WriteString("package probe\n")
	for _, en := range u.Enums {
		members := enumPool
		if len(members) == 0 {
			members = []string{"M0"}
		}
		b.WriteString(fmt.Sprintf("enum %s { %s }\n", en, strings.Join(members, ", ")))
	}
	n := 1 + r.Intn(8)
	b.WriteString("fixed table Root {\n")
	for i := 0; i < n; i++ {
		f := randomField(r, u.Enums)
		u.Fields = append(u.Fields, f)
		b.WriteString(" " + fieldDecl(f, u.Enums) + "\n")
	}
	b.WriteString("}\n")
	return b.String(), u
}

// jsonPool drives text values: escapes, U+2028, astral code points,
// multi-byte UTF-8.
var jsonPool = []string{"hello", "a\"b", "line\nbreak", "\u2028", "🌍", "é\u00e9", "tab\there", "\\backslash", "{}[]"}

func pickText(r *rand.Rand, n int) string {
	s := jsonPool[r.Intn(len(jsonPool))]
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// randomValue writes JSON for one field value into b.
func randomValue(b *strings.Builder, r *rand.Rand, f *genField) {
	switch f.Kind {
	case gInt:
		var v int64
		min, max := f.Imin, f.Imax
		if r.Intn(2) == 0 {
			v = min
		} else {
			v = max
		}
		fmt.Fprintf(b, "%d", v)
	case gUint:
		min, max, _, _ := uintBounds(f.Bits, f.IMode)
		var v uint64
		if r.Intn(2) == 0 {
			v = min
		} else {
			v = max
		}
		fmt.Fprintf(b, "%d", v)
	case gFloat32:
		switch r.Intn(6) {
		case 0:
			b.WriteString("-0")
		case 1:
			b.WriteString("1e-45")
		case 2:
			b.WriteString("3.4028235e38")
		case 3:
			b.WriteString("0.5")
		default:
			fmt.Fprintf(b, "%v", float32(r.NormFloat64()))
		}
	case gFloat64:
		switch r.Intn(6) {
		case 0:
			b.WriteString("-0")
		case 1:
			b.WriteString("5e-324")
		case 2:
			b.WriteString("1.7976931348623157e308")
		case 3:
			b.WriteString("0.25")
		default:
			fmt.Fprintf(b, "%v", r.NormFloat64())
		}
	case gBool:
		if r.Intn(2) == 0 {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case gEnum:
		fmt.Fprintf(b, "%q", f.EnumVal)
	case gString:
		fmt.Fprintf(b, "%q", pickText(r, f.StrN))
	case gBytes:
		fmt.Fprintf(b, "%q", pickText(r, f.StrN))
	case gTimestamp:
		fmt.Fprintf(b, "%d", r.Int63n(1<<40))
	case gScalarArray:
		m := 1 + r.Intn(f.MaxN)
		b.WriteString("[")
		for i := 0; i < m; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			randomValue(b, r, f.Elem)
		}
		b.WriteString("]")
	case gOptScalar:
		if r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			randomValue(b, r, f.Elem)
		}
	}
}

// randomJSON writes the Root value for instance i (0 = {}).
func randomJSON(r *rand.Rand, u *genUnit, i int) string {
	var b strings.Builder
	b.WriteString("{")
	first := true
	if i == 0 {
		b.WriteString("}")
		return b.String()
	}
	for _, f := range u.Fields {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:", f.Name)
		randomValue(&b, r, f)
	}
	b.WriteString("}")
	return b.String()
}

var _ = math.MaxInt8
