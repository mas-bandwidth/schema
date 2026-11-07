package fuzz_test

// tablegen_test.go is the seeded generator half of the random-schema
// differential harness (card schema-go-fuzz-random-table-differential). It
// builds VALID table units from a math/rand seed and writes JSON text for any
// value in range, with the bias to both ends the card names.
//
// The generator only builds constructs the checker accepts: one-sided ranges,
// ranges excluding zero without a default, arrays of bytes/timestamp,
// ?varstring, ? over a variable closure, attributes on a map and text lists in
// fixed tables are all in the campaign's refusal census and never produced
// here. The kinds it covers are the scalars, text, bounded and open lists,
// maps, pointers, nested (inner) tables and unions the harness's round-trip
// and engine legs drive.

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// genKind is the algebraic type of one generated field.
type genKind int

const (
	gInt genKind = iota
	gUint
	gFixed
	gUFixed
	gFloat32
	gFloat64
	gBool
	gEnum
	gString
	gBytes
	gTimestamp
	gScalarArray // [..N]scalar
	gOptScalar   // ?scalar or ?fixedTable
	gNested      // inner table by value
	gMap         // map[K]V
	gPointer     // *Table / *string / *bytes
	gUnion       // union reference
	gFixedArray  // [N]scalar
	gOpenArray   // []scalar
)

// intRange mode for signed/unsigned scalars.
type intMode int

const (
	mNone        intMode = iota // no bounds declared
	mBothZero                   // both ends hold zero
	mExtreme                    // both ends at the implied extremes
	mExclDefault                // exclude zero with an explicit in-range default
)

type genField struct {
	Name   string
	Kind   genKind
	Bits   int // 8/16/32/64 for int/uint
	FixedI int
	FixedF int

	IMode intMode
	Imin  int64
	Imax  int64
	IDef  int64

	EnumVar string // variable name of the enum type
	EnumVal string // a valid member

	StrN int

	Elem *genField // element for scalar array / optional / nested / map value / pointer
	MaxN int       // array max length ([..N])

	NestedName string    // table name for gNested / gPointer
	UnionName  string    // union name for gUnion
	MapKey     *genField // key field for gMap

}

type genTable struct {
	Name      string
	Fixed     bool
	Integrity string
	Fields    []*genField
}

type genUnion struct {
	Name string
	Arms []*genField
}

type genUnit struct {
	Enums  []string
	Unions []*genUnion
	Inner  []*genTable
	Root   *genTable

	// fixedFraction records, per kind and storage-width pair, that a drawn
	// value carried a nonzero fraction bit. tablegen_fixedpoint_test.go reads
	// it as the fraction census.
	fixedFraction map[string]bool
}

func fixedKey(f *genField) string {
	kind := "fixed"
	if f.Kind == gUFixed {
		kind = "ufixed"
	}
	return fmt.Sprintf("%s(%d,%d)", kind, f.FixedI, f.FixedF)
}

// drawFixed draws k in [0, 2^F] and returns k; the value is k / 2^F whole
// units, so a nonzero fraction bit is exactly a nonzero low F bits
// (docs/SPEC.md §4.3). F is at most 16 in fixedPairs.
func drawFixed(r *rand.Rand, frac int) uint64 {
	if frac == 0 {
		return uint64(r.Intn(2))
	}
	limit := uint64(1) << uint(frac)
	return r.Uint64() % (limit + 1)
}

// fixedText spells k / 2^F exactly, independently of tabletext.FormatWide:
// the whole part is k >> F and each of the F fraction digits is one step of
// long division by 2^F, which terminates because the denominator is a power
// of two (docs/SPEC-TABLES.md §16.2, the whole-unit decimal a fixed default
// uses).
func fixedText(k uint64, frac int) string {
	if frac == 0 {
		return strconv.FormatUint(k, 10)
	}
	mask := uint64(1)<<uint(frac) - 1
	var b strings.Builder
	b.WriteString(strconv.FormatUint(k>>uint(frac), 10))
	b.WriteByte('.')
	rem := k & mask
	for range frac {
		rem *= 10
		b.WriteByte(byte('0' + rem>>uint(frac)))
		rem &= mask
	}
	return b.String()
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

// randIntInRange draws uniformly in [lo,hi], handling full-width spans where
// lo and hi are the signed extremes.
func randIntInRange(r *rand.Rand, lo, hi int64) int64 {
	if lo == hi {
		return lo
	}
	span := uint64(hi) - uint64(lo) + 1
	if span == 0 {
		return lo + int64(r.Uint64())
	}
	return lo + int64(r.Uint64()%span)
}

// randUintInRange draws uniformly in [lo,hi], handling full-width spans.
func randUintInRange(r *rand.Rand, lo, hi uint64) uint64 {
	if lo == hi {
		return lo
	}
	span := hi - lo + 1
	if span == 0 {
		return r.Uint64()
	}
	return lo + r.Uint64()%span
}

// randomKey returns a map key field (string or integer).
// randomArrayElem returns an element for a fixed or bounded array. The checker
// refuses arrays of bytes, timestamp and string (a [N]string(N) is a text
// list, a variable-table construct), so only the non-text scalars remain.
func randomArrayElem(r *rand.Rand, enumVars []string) *genField {
	f := &genField{Name: "e"}
	switch r.Intn(6) {
	case 0:
		f.Kind = gInt
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
		f.Imin, f.Imax, f.IDef, _ = intBounds(f.Bits, mNone)
	case 1:
		f.Kind = gUint
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
	case 2:
		f.Kind = gFloat32
	case 3:
		f.Kind = gFloat64
	case 4:
		f.Kind = gBool
	default:
		f.Kind = gEnum
		f.EnumVar = enumVars[r.Intn(len(enumVars))]
		f.EnumVal = enumPool[r.Intn(len(enumPool))]
	}
	return f
}

// randomOptScalar returns a scalar payload for ?T (a fixed table or a non-text
// scalar). The checker refuses ?string(N), ?bytes and ?timestamp.
func randomOptScalar(r *rand.Rand, enumVars []string) *genField {
	f := &genField{Name: "e"}
	switch r.Intn(6) {
	case 0:
		f.Kind = gInt
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
		f.Imin, f.Imax, f.IDef, _ = intBounds(f.Bits, mNone)
	case 1:
		f.Kind = gUint
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
	case 2:
		f.Kind = gFloat32
	case 3:
		f.Kind = gFloat64
	case 4:
		f.Kind = gBool
	default:
		f.Kind = gEnum
		f.EnumVar = enumVars[r.Intn(len(enumVars))]
		f.EnumVal = enumPool[r.Intn(len(enumPool))]
	}
	return f
}

// randomField builds one field of a table, using the already-declared enums,
// unions and inner tables. It only builds constructs that stay inside a FIXED
// table (the harness's probe drives the fixed table's value API): no maps, no
// pointers, no unbounded arrays — those need a variable table and a builder
// probe leg, which is out of scope for this card's core harness. Nested inner
// tables are referenced by value and are themselves fixed.
// fixedPairs are the storage-width pairs accepted by docs/SPEC.md §4.3.
var fixedPairs = [][2]int{{4, 4}, {8, 8}, {16, 16}, {32, 32}, {48, 16}, {24, 8}, {8, 0}}

func randomField(r *rand.Rand, enumVars []string, unions []*genUnion, tables []*genTable) *genField {
	f := &genField{Name: fmt.Sprintf("f%d", r.Intn(100000))}
	var fixedTables []*genTable
	for _, t := range tables {
		if t.Fixed {
			fixedTables = append(fixedTables, t)
		}
	}
	// Preserve the base generator's choices and add seven fixed-point slots
	// so the fixed-point census can cover each pair for both signednesses.
	k := r.Intn(25)
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
		f.Elem = randomArrayElem(r, enumVars)
		f.MaxN = 1 + r.Intn(4)
	case k == 11:
		f.Kind = gOptScalar
		f.Elem = randomOptScalar(r, enumVars)
	case k == 12:
		f.Kind = gFixedArray
		f.Elem = randomArrayElem(r, enumVars)
		f.MaxN = 1 + r.Intn(4)
	case k == 13 && len(fixedTables) > 0:
		t := fixedTables[r.Intn(len(fixedTables))]
		f.Kind = gNested
		f.NestedName = t.Name
	case k == 14 && len(unions) > 0:
		f.Kind = gUnion
		f.UnionName = unions[r.Intn(len(unions))].Name
	case k == 15 && len(fixedTables) > 0:
		t := fixedTables[r.Intn(len(fixedTables))]
		f.Kind = gNested
		f.NestedName = t.Name
	case k == 16 && len(fixedTables) > 0:
		t := fixedTables[r.Intn(len(fixedTables))]
		f.Kind = gOptScalar
		f.Elem = &genField{Kind: gNested, NestedName: t.Name}
	case k >= 18:
		pair := fixedPairs[r.Intn(len(fixedPairs))]
		f.Kind = gFixed
		if r.Intn(2) == 0 {
			f.Kind = gUFixed
		}
		f.FixedI, f.FixedF = pair[0], pair[1]
	default:
		f.Kind = gInt
		f.Bits = []int{8, 32, 64}[r.Intn(3)]
		f.Imin, f.Imax, f.IDef, _ = intBounds(f.Bits, mNone)
	}
	return f
}

func typeName(f *genField, enumVars []string) string {
	switch f.Kind {
	case gInt:
		return fmt.Sprintf("int%d", f.Bits)
	case gUint:
		return fmt.Sprintf("uint%d", f.Bits)
	case gFixed:
		return fmt.Sprintf("fixed(%d, %d)", f.FixedI, f.FixedF)
	case gUFixed:
		return fmt.Sprintf("ufixed(%d, %d)", f.FixedI, f.FixedF)
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
	case gFixedArray:
		return fmt.Sprintf("[%d]%s", f.MaxN, typeName(f.Elem, enumVars))
	case gOpenArray:
		return "[]" + typeName(f.Elem, enumVars)
	case gOptScalar:
		return "?" + typeName(f.Elem, enumVars)
	case gNested:
		return f.NestedName
	case gMap:
		return fmt.Sprintf("map[%s]%s", typeName(f.MapKey, enumVars), typeName(f.Elem, enumVars))
	case gPointer:
		if f.Elem != nil && (f.Elem.Kind == gString || f.Elem.Kind == gBytes) {
			if f.Elem.Kind == gString {
				return "*string"
			}
			return "*bytes"
		}
		return "*" + typeName(f.Elem, enumVars)
	case gUnion:
		return f.UnionName
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
	case gFixed, gUFixed:
		// docs/SPEC.md §4.3: fixed-point bounds are whole units; [0,1] fits every sampled pair.
		return fmt.Sprintf("%s %s | min = 0, max = 1", f.Name, t)
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

// tableSource renders a table declaration (fixed or variable).
func tableSource(t *genTable, enumVars []string) string {
	var b strings.Builder
	if t.Fixed {
		b.WriteString("fixed table " + t.Name)
	} else {
		b.WriteString("table " + t.Name)
	}
	if t.Integrity != "" {
		fmt.Fprintf(&b, " | integrity = %q", t.Integrity)
	}
	b.WriteString(" {\n")
	for _, f := range t.Fields {
		b.WriteString(" " + fieldDecl(f, enumVars) + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func randomSchema(r *rand.Rand) (string, *genUnit) {
	u := &genUnit{}
	ne := 1 + r.Intn(2)
	for i := range ne {
		u.Enums = append(u.Enums, fmt.Sprintf("Enum%d", i))
	}
	var b strings.Builder
	b.WriteString("package probe\n")
	for _, en := range u.Enums {
		fmt.Fprintf(&b, "enum %s { %s }\n", en, strings.Join(enumPool, ", "))
	}
	// inner tables (always fixed), referenced by value from Root or a field
	ni := r.Intn(3)
	for i := range ni {
		t := &genTable{Name: fmt.Sprintf("Inner%d", i), Fixed: true}
		nf := 1 + r.Intn(3)
		for j := range nf {
			f := randomField(r, u.Enums, nil, nil)
			f.Name = fmt.Sprintf("g%d", j)
			t.Fields = append(t.Fields, f)
		}
		u.Inner = append(u.Inner, t)
	}
	// unions: scalar arms only (a pointer arm would force a variable table,
	// which needs a builder probe leg), every one referenced from Root
	nu := r.Intn(3)
	for i := range nu {
		un := &genUnion{Name: fmt.Sprintf("U%d", i)}
		un.Arms = append(un.Arms, &genField{Name: "a", Kind: gInt, Bits: 32})
		un.Arms = append(un.Arms, &genField{Name: "b", Kind: gInt, Bits: 64})
		b.WriteString("union " + un.Name + " {\n")
		for _, arm := range un.Arms {
			b.WriteString(" " + arm.Name + " " + typeName(arm, u.Enums) + "\n")
		}
		b.WriteString("}\n")
		u.Unions = append(u.Unions, un)
	}
	// On a seeded subset of units (about half; deterministic per seed), Root is a
	// variable table declaring `| integrity = "crc32c"`, never a `fixed table` Root.
	// docs/SPEC-TABLES.md §2.14 ("The integrity check: `integrity`"):
	// "The vocabulary today is one value, `crc32c`: CRC-32C, the Castagnoli
	// polynomial (`0x1EDC6F41`, reflected `0x82F63B78`, initial value and
	// final XOR `0xFFFFFFFF`)" ... "WHAT IT DECLARES: the FILE whose ROOT is
	// this table carries the INTEGRITY TRAILER, the variable form's (§3)" ...
	// "The key speaks of the FILE, and so of the ROOT only: the same table
	// nested in another table's body, or reached through a pointer of a
	// pointered save, is a body inside some root's file and carries no trailer
	// of its own. One file has one trailer, governed by its root's declaration."
	// ... "Until then a `fixed table` that declares `integrity` is refused by
	// name at check, since the class carries form `3` and is the one class that
	// rides the message form (§3.3: \"`fixed table T` may ride here, a plain
	// `table T` may not\"). A variable table carries neither, so the key on it
	// is complete. The cook (§7) and the block form (§19) are trusted
	// accelerators whose integrity answer is a signature, and the key does not
	// touch them." ... "Go carries the construct first, and every other target
	// refuses it by name, centrally, at generation (§2.12's rule)".
	root := &genTable{Name: "Root", Fixed: true}
	if r.Intn(2) == 0 {
		root.Fixed = false
		root.Integrity = "crc32c"
	}
	n := 1 + r.Intn(6)
	for i := range n {
		f := randomField(r, u.Enums, u.Unions, u.Inner)
		f.Name = fmt.Sprintf("f%d", i)
		root.Fields = append(root.Fields, f)
	}
	// every union is referenced from Root
	for _, un := range u.Unions {
		ref := false
		for _, f := range root.Fields {
			if f.Kind == gUnion && f.UnionName == un.Name {
				ref = true
				break
			}
		}
		if !ref {
			f := &genField{Name: fmt.Sprintf("u%d", len(root.Fields)), Kind: gUnion, UnionName: un.Name}
			root.Fields = append(root.Fields, f)
		}
	}
	u.Root = root
	for _, t := range u.Inner {
		b.WriteString(tableSource(t, u.Enums))
	}
	b.WriteString(tableSource(root, u.Enums))
	return b.String(), u
}

var jsonPool = []string{"hello", "a\"b", "line\nbreak", "\u2028", "🌍", "é\u00e9", "tab\there", "\\backslash", "{}[]"}

func pickText(r *rand.Rand, n int) string {
	s := jsonPool[r.Intn(len(jsonPool))]
	if len(s) > n {
		cut := n
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

var b64 = base64.StdEncoding

// innerTable finds an inner table by name.
func (u *genUnit) innerTable(name string) *genTable {
	for _, t := range u.Inner {
		if t.Name == name {
			return t
		}
	}
	return nil
}

func (u *genUnit) union(name string) *genUnion {
	for _, un := range u.Unions {
		if un.Name == name {
			return un
		}
	}
	return nil
}

// randomValue writes JSON for one field value into b. It needs the unit for
// nested tables and unions.
func (u *genUnit) randomValue(b *strings.Builder, r *rand.Rand, f *genField) {
	switch f.Kind {
	case gInt:
		min, max := f.Imin, f.Imax
		var v int64
		switch r.Intn(3) {
		case 0:
			v = min
		case 1:
			v = max
		default:
			v = randIntInRange(r, min, max)
		}
		fmt.Fprintf(b, "%d", v)
	case gUint:
		min, max, _, _ := uintBounds(f.Bits, f.IMode)
		var v uint64
		switch r.Intn(3) {
		case 0:
			v = min
		case 1:
			v = max
		default:
			v = randUintInRange(r, min, max)
		}
		fmt.Fprintf(b, "%d", v)
	case gFixed, gUFixed:
		k := drawFixed(r, f.FixedF)
		if f.FixedF > 0 && k%(uint64(1)<<uint(f.FixedF)) != 0 {
			if u.fixedFraction == nil {
				u.fixedFraction = map[string]bool{}
			}
			u.fixedFraction[fixedKey(f)] = true
		}
		b.WriteString(fixedText(k, f.FixedF))
	// docs/SPEC.md §4.3 defines the float widths; include both signs at their edges.
	case gFloat32:
		switch r.Intn(8) {
		case 0:
			b.WriteString("-0")
		case 1:
			b.WriteString("1e-45")
		case 2:
			b.WriteString("-1e-45")
		case 3:
			b.WriteString("3.4028234663852886e38")
		case 4:
			b.WriteString("-3.4028234663852886e38")
		case 5:
			b.WriteString("0")
		case 6:
			b.WriteString("0.5")
		default:
			fmt.Fprintf(b, "%v", float32(r.NormFloat64()))
		}
	case gFloat64:
		switch r.Intn(8) {
		case 0:
			b.WriteString("-0")
		case 1:
			b.WriteString("5e-324")
		case 2:
			b.WriteString("-5e-324")
		case 3:
			b.WriteString("1.7976931348623157e308")
		case 4:
			b.WriteString("-1.7976931348623157e308")
		case 5:
			b.WriteString("0")
		case 6:
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
		nb := 1 + r.Intn(f.StrN)
		raw := make([]byte, nb)
		if _, err := r.Read(raw); err != nil {
			b.WriteString(`""`)
			break
		}
		fmt.Fprintf(b, "%q", b64.EncodeToString(raw))
	case gTimestamp:
		// docs/SPEC-TABLES.md §16.2: the text form of a timestamp is a
		// strict RFC 3339 string, never its integer nanosecond count (a
		// NUMBER token is a kind mismatch, and the strict read refuses it).
		fmt.Fprintf(b, "%q", time.Unix(0, r.Int63n(1<<40)).UTC().Format(time.RFC3339Nano))
	case gScalarArray, gFixedArray, gOpenArray:
		m := f.MaxN
		if f.Kind == gScalarArray || f.Kind == gOpenArray {
			m = 1 + r.Intn(f.MaxN)
		}
		b.WriteString("[")
		for i := 0; i < m; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			u.randomValue(b, r, f.Elem)
		}
		b.WriteString("]")
	case gOptScalar:
		if r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			u.randomValue(b, r, f.Elem)
		}
	case gNested:
		u.writeTableValue(b, r, u.innerTable(f.NestedName))
	case gMap:
		nk := 1 + r.Intn(3)
		b.WriteString("{")
		for i := range nk {
			if i > 0 {
				b.WriteString(",")
			}
			u.writeKey(b, r, f.MapKey)
			b.WriteString(":")
			u.randomValue(b, r, f.Elem)
		}
		b.WriteString("}")
	case gPointer:
		switch f.Elem.Kind {
		case gNested:
			u.writeTableValue(b, r, u.innerTable(f.Elem.NestedName))
		case gString:
			fmt.Fprintf(b, "%q", pickText(r, f.Elem.StrN))
		case gBytes:
			nb := 1 + r.Intn(f.Elem.StrN)
			raw := make([]byte, nb)
			if _, err := r.Read(raw); err != nil {
				b.WriteString(`""`)
				break
			}
			fmt.Fprintf(b, "%q", b64.EncodeToString(raw))
		default:
			u.randomValue(b, r, f.Elem)
		}
	case gUnion:
		un := u.union(f.UnionName)
		if un == nil || len(un.Arms) == 0 || r.Intn(4) == 0 {
			b.WriteString("{}")
			break
		}
		arm := un.Arms[r.Intn(len(un.Arms))]
		fmt.Fprintf(b, "{%q:", arm.Name)
		u.randomValue(b, r, arm)
		b.WriteString("}")
	}
}

// writeKey writes a JSON map key.
func (u *genUnit) writeKey(b *strings.Builder, r *rand.Rand, f *genField) {
	switch f.Kind {
	case gString:
		fmt.Fprintf(b, "%q", pickText(r, f.StrN))
	case gInt:
		v := randIntInRange(r, f.Imin, f.Imax)
		fmt.Fprintf(b, "%q", strconv.FormatInt(v, 10))
	case gUint:
		min, max, _, _ := uintBounds(f.Bits, f.IMode)
		v := randUintInRange(r, min, max)
		fmt.Fprintf(b, "%q", strconv.FormatUint(v, 10))
	default:
		fmt.Fprintf(b, "%q", "k")
	}
}

// writeTableValue writes a table object value with every field set.
func (u *genUnit) writeTableValue(b *strings.Builder, r *rand.Rand, t *genTable) {
	b.WriteString("{")
	first := true
	for _, f := range t.Fields {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(b, "%q:", f.Name)
		u.randomValue(b, r, f)
	}
	b.WriteString("}")
}

func randomJSON(r *rand.Rand, u *genUnit, i int) string {
	var b strings.Builder
	b.WriteString("{")
	if i == 0 {
		b.WriteString("}")
		return b.String()
	}
	first := true
	for _, f := range u.Root.Fields {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:", f.Name)
		u.randomValue(&b, r, f)
	}
	b.WriteString("}")
	return b.String()
}

// Census reports the schema constructs present in this unit for
// TestRandomTableGenerator's coverage check over seeds 0..399.
func (u *genUnit) Census() map[string]bool {
	c := map[string]bool{}
	if u.Root.Integrity != "" {
		c["integrity"] = true
	} else {
		c["no integrity"] = true
	}
	if u.Root.Fixed {
		c["fixed Root"] = true
	} else {
		c["variable Root"] = true
	}
	if len(u.Inner) > 0 {
		c["inner table"] = true
	}
	if len(u.Unions) > 0 {
		c["union"] = true
	}
	var visitField func(f *genField)
	visitField = func(f *genField) {
		if f == nil {
			return
		}
		switch f.Kind {
		case gInt:
			c["int"] = true
			c[fmt.Sprintf("range mode %d", f.IMode)] = true
		case gUint:
			c["uint"] = true
			c[fmt.Sprintf("range mode %d", f.IMode)] = true
		case gFixed:
			c[fmt.Sprintf("fixed(%d,%d)", f.FixedI, f.FixedF)] = true
		case gUFixed:
			c[fmt.Sprintf("ufixed(%d,%d)", f.FixedI, f.FixedF)] = true
		case gFloat32:
			c["float32"] = true
		case gFloat64:
			c["float64"] = true
		case gBool:
			c["bool"] = true
		case gEnum:
			c["enum"] = true
		case gString:
			c["string(N)"] = true
		case gBytes:
			c["bytes(N)"] = true
		case gTimestamp:
			c["timestamp"] = true
		case gScalarArray:
			c["[..N]T"] = true
			visitField(f.Elem)
		case gFixedArray:
			c["[N]T"] = true
			visitField(f.Elem)
		case gOptScalar:
			c["?T"] = true
			visitField(f.Elem)
		case gNested:
			c["nested table"] = true
		case gUnion:
			c["union"] = true
		}
	}
	for _, t := range u.Inner {
		for _, f := range t.Fields {
			visitField(f)
		}
	}
	for _, f := range u.Root.Fields {
		visitField(f)
	}
	return c
}

var _ = math.MaxInt8
