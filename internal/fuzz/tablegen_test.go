package fuzz_test

// tablegen_test.go is the seeded generator half of the random-schema
// differential harness (card schema-go-fuzz-random-table-differential). From a
// math/rand seed (no time) it builds a VALID table unit: 0-2 enums, 0-3 inner
// tables (each fixed or variable), a Root (fixed or variable), up to 8 fields
// per table over the field grammar of docs/SPEC-TABLES.md §2, and unions with a
// pointer, a *string and an int arm, every union referenced from Root. It then
// writes JSON text (§16.2) for any value of any table, in range, with a bias to
// both ends.
//
// It never builds a construct the checker refuses (the campaign's refusal
// census): a one-sided range (§4.3), a range excluding zero without a default,
// an array of bytes(N) or timestamp, ?string(N), ? over a variable closure,
// attributes on a map, text lists in a fixed table. TestRandomTableGenerator
// pins that: every unit of the first 400 seeds passes the checker.

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type gKind int

const (
	gInt gKind = iota
	gUint
	gFloat32
	gFloat64
	gBool
	gEnum
	gFixedPoint  // fixed(I, F) / ufixed(I, F), §16.2 whole units
	gString      // string(N)
	gBytes       // bytes(N), base64 in text
	gTimestamp   // RFC 3339 text, §2.11
	gArray       // [N]T
	gBounded     // [..N]T
	gList        // []T, §2.9
	gTextBounded // [..N]string(M) / [..N]?string(M), §2.13
	gTextList    // []string(M) / []?string(M), §2.13
	gMapString   // map[string(N)]V, §2.8
	gMapInt      // map[intK]V, §2.8
	gOptional    // ?T, §2.3
	gByValue     // a table by value
	gPtrTable    // *T, §2.1
	gPtrString   // *string, §2.5
	gPtrBytes    // *bytes, §2.5
	gUnionField  // §2.6
	gPing        // a payload-free union arm
)

// rangeMode is how an integer or fixed-point field declares its range.
type rangeMode int

const (
	rNone     rangeMode = iota // no bounds declared: the implied range
	rHoldZero                  // both ends hold zero: min <= 0 <= max
	rExtremes                  // both ends at the implied extremes, declared
	rExclZero                  // excluding zero, with an explicit in-range default
)

type gEnumDecl struct {
	Name    string
	Members []string
}

type gType struct {
	Kind     gKind
	Bits     int  // integers: 8..64; fixed point: I+F
	Unsigned bool // fixed point: ufixed
	I, F     int  // fixed point
	Range    rangeMode
	Lo, Hi   *big.Int // the value range (whole units for fixed point), always set for numbers
	Def      *big.Int // explicit default (rExclZero)
	N        int      // string/bytes bound, array count or bound, map key bound
	M        int      // text list element bound
	OptElem  bool     // text list of ?string(M)
	Elem     *gType   // array/list/optional/map value
	KeyBits  int      // map[intK]: key bits
	KeyUns   bool     // map[uintK]
	Enum     *gEnumDecl
	Table    *gTable
	Union    *gUnion
}

type gField struct {
	Name string
	T    *gType
}

type gTable struct {
	Name   string
	Fixed  bool
	Fields []*gField
}

type gUnion struct {
	Name string
	Arms []*gField
}

type genUnit struct {
	Seed   int
	Enums  []*gEnumDecl
	Tables []*gTable // inner tables, declaration order; table i names only j < i
	Unions []*gUnion
	Root   *gTable
}

var enumMemberPool = []string{"Alpha", "Beta", "Gamma", "Delta", "Omega"}

func implied(bits int, unsigned bool) (*big.Int, *big.Int) {
	if unsigned {
		hi := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		return big.NewInt(0), hi.Sub(hi, big.NewInt(1))
	}
	hi := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	lo := new(big.Int).Neg(hi)
	return lo, hi.Sub(hi, big.NewInt(1))
}

// randBig draws an integer in [lo, hi], biased to both ends.
func randBig(r *rand.Rand, lo, hi *big.Int) *big.Int {
	span := new(big.Int).Sub(hi, lo)
	switch r.Intn(6) {
	case 0:
		return new(big.Int).Set(lo)
	case 1:
		return new(big.Int).Set(hi)
	case 2:
		if span.Cmp(big.NewInt(1)) >= 0 {
			return new(big.Int).Add(lo, big.NewInt(1))
		}
		return new(big.Int).Set(lo)
	case 3:
		if span.Cmp(big.NewInt(1)) >= 0 {
			return new(big.Int).Sub(hi, big.NewInt(1))
		}
		return new(big.Int).Set(hi)
	case 4:
		if lo.Sign() <= 0 && hi.Sign() >= 0 {
			return big.NewInt(0)
		}
	}
	n := new(big.Int).Rand(r, new(big.Int).Add(span, big.NewInt(1)))
	return n.Add(n, lo)
}

// numericRange gives an integer or fixed-point type its declared range (§4,
// §4.3: two-sided only; a range excluding zero carries an explicit default).
func numericRange(r *rand.Rand, t *gType, lo, hi *big.Int, allowExcl bool) {
	t.Lo, t.Hi = lo, hi
	modes := []rangeMode{rNone, rHoldZero, rExtremes}
	if allowExcl {
		modes = append(modes, rExclZero)
	}
	t.Range = modes[r.Intn(len(modes))]
	switch t.Range {
	case rHoldZero:
		// min <= 0 <= max, each end drawn inside the implied range
		a := new(big.Int).Rand(r, new(big.Int).Add(new(big.Int).Neg(lo), big.NewInt(1)))
		b := new(big.Int).Rand(r, new(big.Int).Add(hi, big.NewInt(1)))
		if r.Intn(4) == 0 {
			a = big.NewInt(0)
		}
		if r.Intn(4) == 0 && lo.Sign() < 0 {
			b = big.NewInt(0)
		}
		t.Lo, t.Hi = a.Neg(a), b
		if t.Lo.Cmp(t.Hi) == 0 {
			t.Hi = new(big.Int).Add(t.Hi, big.NewInt(1))
		}
	case rExclZero:
		// both ends above zero, the default in range
		a := new(big.Int).Rand(r, hi)
		a.Add(a, big.NewInt(1))
		b := new(big.Int).Sub(hi, a)
		b.Rand(r, b.Add(b, big.NewInt(1)))
		b.Add(b, a)
		t.Lo, t.Hi = a, b
		t.Def = randBig(r, a, b)
	}
}

type gen struct {
	r     *rand.Rand
	u     *genUnit
	names int
}

func (g *gen) intType(allowRange, allowExcl bool) *gType {
	t := &gType{Kind: gInt, Bits: []int{8, 16, 32, 64}[g.r.Intn(4)]}
	if g.r.Intn(3) == 0 {
		t.Kind = gUint
	}
	lo, hi := implied(t.Bits, t.Kind == gUint)
	if allowRange {
		numericRange(g.r, t, lo, hi, allowExcl)
	} else {
		t.Lo, t.Hi = lo, hi
	}
	return t
}

var fixedPairs = [][2]int{{4, 4}, {8, 8}, {16, 16}, {32, 32}, {48, 16}, {24, 8}, {8, 0}}

func (g *gen) fixedPointType() *gType {
	p := fixedPairs[g.r.Intn(len(fixedPairs))]
	t := &gType{Kind: gFixedPoint, I: p[0], F: p[1], Bits: p[0] + p[1], Unsigned: g.r.Intn(2) == 0}
	lo, hi := implied(t.I, t.Unsigned)
	// whole-unit bounds are required (SPEC §4.3); hold zero or sit at the extremes
	t.Lo, t.Hi = lo, hi
	t.Range = rExtremes
	if g.r.Intn(2) == 0 {
		t.Range = rHoldZero
		a := new(big.Int).Rand(g.r, new(big.Int).Add(new(big.Int).Neg(lo), big.NewInt(1)))
		b := new(big.Int).Rand(g.r, new(big.Int).Add(hi, big.NewInt(1)))
		t.Lo, t.Hi = a.Neg(a), b
		if t.Lo.Cmp(t.Hi) == 0 {
			t.Hi = new(big.Int).Add(t.Hi, big.NewInt(1))
		}
	}
	return t
}

func (g *gen) enumType() *gType {
	if len(g.u.Enums) == 0 {
		return &gType{Kind: gBool}
	}
	return &gType{Kind: gEnum, Enum: g.u.Enums[g.r.Intn(len(g.u.Enums))]}
}

// scalar is an element kind any array, optional or map value may take.
func (g *gen) scalar() *gType {
	switch g.r.Intn(6) {
	case 0, 1:
		return g.intType(false, false)
	case 2:
		return &gType{Kind: gFloat32}
	case 3:
		return &gType{Kind: gFloat64}
	case 4:
		return &gType{Kind: gBool}
	default:
		return g.enumType()
	}
}

// earlier returns the inner tables a table at index idx may name, optionally
// only fixed ones.
func (g *gen) earlier(idx int, fixedOnly, variableOnly bool) []*gTable {
	var out []*gTable
	for i, t := range g.u.Tables {
		if idx >= 0 && i >= idx {
			break
		}
		if fixedOnly && !t.Fixed || variableOnly && t.Fixed {
			continue
		}
		out = append(out, t)
	}
	return out
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.Intn(len(xs))] }

// elemOrTable is an array element: a scalar or a fixed table.
func (g *gen) elemOrTable(idx int, allowVariable bool) *gType {
	if fx := g.earlier(idx, !allowVariable, false); len(fx) > 0 && g.r.Intn(3) == 0 {
		return &gType{Kind: gByValue, Table: pick(g.r, fx)}
	}
	return g.scalar()
}

// fieldType draws one field type legal in a table of the given class.
func (g *gen) fieldType(idx int, fixed bool) *gType {
	for {
		k := g.r.Intn(24)
		switch k {
		case 0, 1:
			return g.intType(true, true)
		case 2:
			return &gType{Kind: gFloat32}
		case 3:
			return &gType{Kind: gFloat64}
		case 4:
			return &gType{Kind: gBool}
		case 5:
			return g.enumType()
		case 6:
			return g.fixedPointType()
		case 7:
			return &gType{Kind: gString, N: 2 + g.r.Intn(15)}
		case 8:
			return &gType{Kind: gBytes, N: 1 + g.r.Intn(8)}
		case 9:
			return &gType{Kind: gTimestamp}
		case 10:
			return &gType{Kind: gArray, N: 1 + g.r.Intn(4), Elem: g.elemOrTable(idx, false)}
		case 11:
			return &gType{Kind: gBounded, N: 1 + g.r.Intn(4), Elem: g.elemOrTable(idx, false)}
		case 12:
			if fx := g.earlier(idx, true, false); len(fx) > 0 && g.r.Intn(2) == 0 {
				return &gType{Kind: gOptional, Elem: &gType{Kind: gByValue, Table: pick(g.r, fx)}}
			}
			return &gType{Kind: gOptional, Elem: g.scalar()}
		case 13:
			if fixed {
				if fx := g.earlier(idx, true, false); len(fx) > 0 {
					return &gType{Kind: gByValue, Table: pick(g.r, fx)}
				}
				continue
			}
			if all := g.earlier(idx, false, false); len(all) > 0 {
				return &gType{Kind: gByValue, Table: pick(g.r, all)}
			}
			continue
		}
		if fixed {
			continue
		}
		switch k {
		case 14:
			return &gType{Kind: gList, Elem: g.elemOrTable(idx, true)}
		case 15:
			return &gType{Kind: gTextBounded, N: 1 + g.r.Intn(4), M: 2 + g.r.Intn(10), OptElem: g.r.Intn(2) == 0}
		case 16:
			return &gType{Kind: gTextList, M: 2 + g.r.Intn(10), OptElem: g.r.Intn(2) == 0}
		case 17:
			return &gType{Kind: gMapString, N: 2 + g.r.Intn(10), Elem: g.mapValue(idx)}
		case 18:
			t := &gType{Kind: gMapInt, KeyBits: []int{8, 16, 32, 64}[g.r.Intn(4)], KeyUns: g.r.Intn(2) == 0, Elem: g.mapValue(idx)}
			return t
		case 19:
			if vs := g.earlier(idx, false, true); len(vs) > 0 {
				return &gType{Kind: gPtrTable, Table: pick(g.r, vs)}
			}
		case 20:
			return &gType{Kind: gPtrString}
		case 21:
			return &gType{Kind: gPtrBytes}
		}
	}
}

func (g *gen) mapValue(idx int) *gType {
	switch g.r.Intn(4) {
	case 0:
		if fx := g.earlier(idx, true, false); len(fx) > 0 {
			return &gType{Kind: gByValue, Table: pick(g.r, fx)}
		}
	case 1:
		return &gType{Kind: gString, N: 2 + g.r.Intn(10)}
	}
	return g.scalar()
}

func (g *gen) table(name string, idx int, fixed bool) *gTable {
	t := &gTable{Name: name, Fixed: fixed}
	n := 1 + g.r.Intn(8)
	for i := 0; i < n; i++ {
		t.Fields = append(t.Fields, &gField{Name: fmt.Sprintf("f%d", i), T: g.fieldType(idx, fixed)})
	}
	return t
}

// union builds a union with a pointer arm, a *string arm and an int arm
// (§2.6), and up to two more arms drawn from scalars, text, a fixed table and
// a payload-free arm.
func (g *gen) union(name string) *gUnion {
	un := &gUnion{Name: name}
	var vs []*gTable
	for _, t := range g.u.Tables {
		if !t.Fixed {
			vs = append(vs, t)
		}
	}
	if len(vs) > 0 {
		un.Arms = append(un.Arms, &gField{Name: "ptr", T: &gType{Kind: gPtrTable, Table: pick(g.r, vs)}})
	} else {
		un.Arms = append(un.Arms, &gField{Name: "ptr", T: &gType{Kind: gPtrBytes}})
	}
	un.Arms = append(un.Arms, &gField{Name: "label", T: &gType{Kind: gPtrString}})
	un.Arms = append(un.Arms, &gField{Name: "tally", T: g.intType(true, false)})
	for i := g.r.Intn(3); i > 0; i-- {
		switch g.r.Intn(4) {
		case 0:
			un.Arms = append(un.Arms, &gField{Name: fmt.Sprintf("a%d", i), T: g.scalar()})
		case 1:
			un.Arms = append(un.Arms, &gField{Name: fmt.Sprintf("a%d", i), T: &gType{Kind: gString, N: 2 + g.r.Intn(10)}})
		case 2:
			if fx := g.earlier(-1, true, false); len(fx) > 0 {
				un.Arms = append(un.Arms, &gField{Name: fmt.Sprintf("a%d", i), T: &gType{Kind: gByValue, Table: pick(g.r, fx)}})
			}
		default:
			un.Arms = append(un.Arms, &gField{Name: fmt.Sprintf("a%d", i), T: &gType{Kind: gPing}})
		}
	}
	return un
}

// randomUnit builds the unit for one seed.
func randomUnit(seed int) *genUnit {
	g := &gen{r: rand.New(rand.NewSource(int64(seed))), u: &genUnit{Seed: seed}}
	for i, n := 0, g.r.Intn(3); i < n; i++ {
		m := 1 + g.r.Intn(len(enumMemberPool))
		g.u.Enums = append(g.u.Enums, &gEnumDecl{Name: fmt.Sprintf("E%d", i), Members: enumMemberPool[:m]})
	}
	for i, n := 0, g.r.Intn(4); i < n; i++ {
		g.u.Tables = append(g.u.Tables, g.table(fmt.Sprintf("T%d", i), i, g.r.Intn(2) == 0))
	}
	rootFixed := g.r.Intn(3) == 0
	g.u.Root = g.table("Root", len(g.u.Tables), rootFixed)
	if !rootFixed {
		for i, n := 0, g.r.Intn(3); i < n; i++ {
			un := g.union(fmt.Sprintf("U%d", i))
			g.u.Unions = append(g.u.Unions, un)
			g.u.Root.Fields = append(g.u.Root.Fields, &gField{Name: fmt.Sprintf("u%d", i), T: &gType{Kind: gUnionField, Union: un}})
		}
	}
	return g.u
}

func (t *gType) decl() string {
	switch t.Kind {
	case gInt:
		return fmt.Sprintf("int%d", t.Bits)
	case gUint:
		return fmt.Sprintf("uint%d", t.Bits)
	case gFloat32:
		return "float32"
	case gFloat64:
		return "float64"
	case gBool:
		return "bool"
	case gEnum:
		return t.Enum.Name
	case gFixedPoint:
		if t.Unsigned {
			return fmt.Sprintf("ufixed(%d, %d)", t.I, t.F)
		}
		return fmt.Sprintf("fixed(%d, %d)", t.I, t.F)
	case gString:
		return fmt.Sprintf("string(%d)", t.N)
	case gBytes:
		return fmt.Sprintf("bytes(%d)", t.N)
	case gTimestamp:
		return "timestamp"
	case gArray:
		return fmt.Sprintf("[%d]%s", t.N, t.Elem.decl())
	case gBounded:
		return fmt.Sprintf("[..%d]%s", t.N, t.Elem.decl())
	case gList:
		return "[]" + t.Elem.decl()
	case gTextBounded:
		return fmt.Sprintf("[..%d]%sstring(%d)", t.N, optMark(t.OptElem), t.M)
	case gTextList:
		return fmt.Sprintf("[]%sstring(%d)", optMark(t.OptElem), t.M)
	case gMapString:
		return fmt.Sprintf("map[string(%d)]%s", t.N, t.Elem.decl())
	case gMapInt:
		if t.KeyUns {
			return fmt.Sprintf("map[uint%d]%s", t.KeyBits, t.Elem.decl())
		}
		return fmt.Sprintf("map[int%d]%s", t.KeyBits, t.Elem.decl())
	case gOptional:
		return "?" + t.Elem.decl()
	case gByValue:
		return t.Table.Name
	case gPtrTable:
		return "*" + t.Table.Name
	case gPtrString:
		return "*string"
	case gPtrBytes:
		return "*bytes"
	case gUnionField:
		return t.Union.Name
	}
	return ""
}

func optMark(b bool) string {
	if b {
		return "?"
	}
	return ""
}

func fieldLine(f *gField) string {
	if f.T.Kind == gPing {
		return f.Name
	}
	s := f.Name + " " + f.T.decl()
	if f.T.Def != nil {
		s += " = " + f.T.Def.String()
	}
	if f.T.Range != rNone && (f.T.Kind == gInt || f.T.Kind == gUint || f.T.Kind == gFixedPoint) {
		s += fmt.Sprintf(" | min = %s, max = %s", f.T.Lo, f.T.Hi)
	}
	return s
}

// Source writes the unit as schema source.
func (u *genUnit) Source() string {
	var b strings.Builder
	b.WriteString("package probe\n")
	for _, e := range u.Enums {
		fmt.Fprintf(&b, "enum %s {\n", e.Name)
		for _, m := range e.Members {
			fmt.Fprintf(&b, " %s\n", m)
		}
		b.WriteString("}\n")
	}
	tbl := func(t *gTable) {
		if t.Fixed {
			b.WriteString("fixed ")
		}
		fmt.Fprintf(&b, "table %s {\n", t.Name)
		for _, f := range t.Fields {
			b.WriteString(" " + fieldLine(f) + "\n")
		}
		b.WriteString("}\n")
	}
	for _, t := range u.Tables {
		tbl(t)
	}
	for _, un := range u.Unions {
		fmt.Fprintf(&b, "union %s {\n", un.Name)
		for _, a := range un.Arms {
			b.WriteString(" " + fieldLine(a) + "\n")
		}
		b.WriteString("}\n")
	}
	tbl(u.Root)
	return b.String()
}

func (u *genUnit) has(pred func(*gType) bool) bool {
	seen := map[*gTable]bool{}
	var walkT func(t *gType) bool
	var walkTable func(t *gTable) bool
	walkTable = func(t *gTable) bool {
		if seen[t] {
			return false
		}
		seen[t] = true
		for _, f := range t.Fields {
			if walkT(f.T) {
				return true
			}
		}
		return false
	}
	walkT = func(t *gType) bool {
		if t == nil {
			return false
		}
		if pred(t) {
			return true
		}
		if walkT(t.Elem) {
			return true
		}
		if t.Table != nil && walkTable(t.Table) {
			return true
		}
		if t.Union != nil {
			for _, a := range t.Union.Arms {
				if walkT(a.T) {
					return true
				}
			}
		}
		return false
	}
	return walkTable(u.Root)
}

// Census names the constructs the unit holds, for the breadth report.
func (u *genUnit) Census() map[string]bool {
	c := map[string]bool{}
	if u.Root.Fixed {
		c["fixed Root"] = true
	} else {
		c["variable Root"] = true
	}
	for _, t := range u.Tables {
		if t.Fixed {
			c["fixed inner table"] = true
		} else {
			c["variable inner table"] = true
		}
	}
	if len(u.Enums) > 0 {
		c["enum"] = true
	}
	names := map[gKind]string{gInt: "int", gUint: "uint", gFloat32: "float32", gFloat64: "float64", gBool: "bool", gEnum: "enum field",
		gFixedPoint: "fixed/ufixed", gString: "string(N)", gBytes: "bytes(N)", gTimestamp: "timestamp", gArray: "[N]T", gBounded: "[..N]T",
		gList: "[]T", gTextBounded: "[..N]string(M)", gTextList: "[]string(M)", gMapString: "map[string(N)]V", gMapInt: "map[intK]V",
		gOptional: "?T", gByValue: "table by value", gPtrTable: "*Table", gPtrString: "*string", gPtrBytes: "*bytes", gUnionField: "union", gPing: "payload-free arm"}
	u.has(func(t *gType) bool {
		c[names[t.Kind]] = true
		if t.Kind == gInt || t.Kind == gUint {
			c[fmt.Sprintf("range mode %d", t.Range)] = true
		}
		if (t.Kind == gTextBounded || t.Kind == gTextList) && t.OptElem {
			c["?string(M) list"] = true
		}
		return false
	})
	return c
}

// textPool holds escapes, U+2028/U+2029, astral code points and multi-byte
// UTF-8. It holds no U+0000: the zero byte's rule is owed by
// schema-go-text-zero-byte-one-rule and its engine half by
// schema-tablepack-refuses-zero-byte-text.
var textPool = []string{"", "a", "hello", "a\"b", "back\\slash", "line\nbreak", "tab\there", "\u2028", "\u2029x", "é", "日本語", "🌍", "a🌍b", "\u007f", "\u0001", "/", "{}[]", "Ωmega", "\U0010FFFF"}

// clampText cuts s to at most n bytes at a code point boundary.
func clampText(s string, n int) string {
	if n < 0 || len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (g *gen) text(n int) string {
	s := pick(g.r, textPool)
	if g.r.Intn(3) == 0 {
		s += pick(g.r, textPool)
	}
	return clampText(s, n)
}

// jsonString writes s as a JSON string, sometimes spelling non-ASCII as \u
// escapes (astral code points as a surrogate pair) so the reader's escape
// paths are reached.
func (g *gen) jsonString(b *strings.Builder, s string) {
	esc := g.r.Intn(3) == 0
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			fmt.Fprintf(b, `\u%04x`, c)
		case esc && c > 0x7f && c < 0x10000:
			fmt.Fprintf(b, `\u%04x`, c)
		case esc && c >= 0x10000:
			c -= 0x10000
			fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(c>>10), 0xdc00+(c&0x3ff))
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
}

// exactDecimal writes raw / 2^f as the exact decimal (a dyadic fraction has at
// most f decimal digits).
func exactDecimal(raw *big.Int, f int) string {
	if f == 0 {
		return raw.String()
	}
	q := new(big.Rat).SetFrac(raw, new(big.Int).Lsh(big.NewInt(1), uint(f)))
	return q.FloatString(f)
}

func (g *gen) float32Text() string {
	switch g.r.Intn(8) {
	case 0:
		return "-0"
	case 1:
		return "1e-45" // the smallest subnormal
	case 2:
		return "3.4028235e+38" // the largest finite
	case 3:
		return "-3.4028235e+38"
	case 4:
		return "1.1754942e-38" // the largest subnormal
	case 5:
		return "0"
	}
	for {
		f := math.Float32frombits(g.r.Uint32())
		if !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) {
			return strconv.FormatFloat(float64(f), 'g', -1, 32)
		}
	}
}

func (g *gen) float64Text() string {
	switch g.r.Intn(8) {
	case 0:
		return "-0"
	case 1:
		return "5e-324"
	case 2:
		return "1.7976931348623157e+308"
	case 3:
		return "-1.7976931348623157e+308"
	case 4:
		return "2.225073858507201e-308"
	case 5:
		return "0.1"
	}
	for {
		f := math.Float64frombits(g.r.Uint64())
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
	}
}

// value writes JSON text for one value of t (§16.2), in range.
func (g *gen) value(b *strings.Builder, t *gType, depth int) {
	switch t.Kind {
	case gInt, gUint:
		b.WriteString(randBig(g.r, t.Lo, t.Hi).String())
	case gFloat32:
		b.WriteString(g.float32Text())
	case gFloat64:
		b.WriteString(g.float64Text())
	case gBool:
		b.WriteString([]string{"true", "false"}[g.r.Intn(2)])
	case gEnum:
		g.jsonString(b, pick(g.r, t.Enum.Members))
	case gFixedPoint:
		scale := new(big.Int).Lsh(big.NewInt(1), uint(t.F))
		lo := new(big.Int).Mul(t.Lo, scale)
		hi := new(big.Int).Mul(t.Hi, scale)
		b.WriteString(exactDecimal(randBig(g.r, lo, hi), t.F))
	case gString:
		g.jsonString(b, g.text(t.N))
	case gBytes:
		raw := make([]byte, g.r.Intn(t.N+1))
		g.r.Read(raw)
		b.WriteString(`"` + base64.StdEncoding.EncodeToString(raw) + `"`)
	case gTimestamp:
		var ns int64
		switch g.r.Intn(4) {
		case 0:
			ns = 0
		case 1:
			ns = g.r.Int63n(1 << 62)
		case 2:
			ns = -g.r.Int63n(1 << 62)
		default:
			ns = g.r.Int63n(4e18)
		}
		b.WriteString(`"` + time.Unix(0, ns).UTC().Format(time.RFC3339Nano) + `"`)
	case gArray, gBounded, gList:
		n := t.N
		if t.Kind != gArray {
			n = g.r.Intn(t.N + 1)
		}
		if t.Kind == gList {
			n = g.r.Intn(5)
		}
		if depth > 4 {
			n = 0
			if t.Kind == gArray {
				n = t.N
			}
		}
		b.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			g.value(b, t.Elem, depth+1)
		}
		b.WriteByte(']')
	case gTextBounded, gTextList:
		n := g.r.Intn(5)
		if t.Kind == gTextBounded {
			n = g.r.Intn(t.N + 1)
		}
		b.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			if t.OptElem && g.r.Intn(3) == 0 {
				b.WriteString("null")
			} else {
				g.jsonString(b, g.text(t.M))
			}
		}
		b.WriteByte(']')
	case gMapString, gMapInt:
		n := g.r.Intn(4)
		seen := map[string]bool{}
		b.WriteByte('{')
		first := true
		for i := 0; i < n; i++ {
			var key string
			if t.Kind == gMapString {
				key = g.text(t.N)
			} else {
				lo, hi := implied(t.KeyBits, t.KeyUns)
				key = randBig(g.r, lo, hi).String()
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			if !first {
				b.WriteByte(',')
			}
			first = false
			g.jsonString(b, key)
			b.WriteByte(':')
			g.value(b, t.Elem, depth+1)
		}
		b.WriteByte('}')
	case gOptional:
		if g.r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			g.value(b, t.Elem, depth+1)
		}
	case gByValue:
		g.object(b, t.Table, depth+1)
	case gPtrTable:
		if depth > 4 || g.r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			g.object(b, t.Table, depth+1)
		}
	case gPtrString:
		if g.r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			g.jsonString(b, g.text(-1))
		}
	case gPtrBytes:
		if g.r.Intn(3) == 0 {
			b.WriteString("null")
		} else {
			raw := make([]byte, g.r.Intn(12))
			g.r.Read(raw)
			b.WriteString(`"` + base64.StdEncoding.EncodeToString(raw) + `"`)
		}
	case gUnionField:
		if g.r.Intn(4) == 0 {
			b.WriteString("{}")
			return
		}
		arm := pick(g.r, t.Union.Arms)
		b.WriteString(`{"` + arm.Name + `":`)
		if arm.T.Kind == gPing {
			b.WriteString("null")
		} else {
			g.value(b, arm.T, depth+1)
		}
		b.WriteByte('}')
	case gPing:
		b.WriteString("null")
	}
}

func (g *gen) object(b *strings.Builder, t *gTable, depth int) {
	b.WriteByte('{')
	first := true
	for _, f := range t.Fields {
		if g.r.Intn(6) == 0 {
			continue // an absent key keeps the default
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"` + f.Name + `":`)
		g.value(b, f.T, depth)
	}
	b.WriteByte('}')
}

// Values writes n Root values as JSON text; value 0 is {}.
func (u *genUnit) Values(n int) []string {
	g := &gen{r: rand.New(rand.NewSource(int64(u.Seed)*7919 + 1)), u: u}
	out := []string{"{}"}
	for i := 1; i < n; i++ {
		var b strings.Builder
		g.object(&b, u.Root, 0)
		out = append(out, b.String())
	}
	return out
}

// boundsTokens are the bounds-pass plants for one scalar Root field: min-1,
// max+1, the implied extremes plus one, 1e400, 2.5, "7" and null. Each carries
// whether a silent read of it is wrong (true: the read must count something).
type boundsToken struct {
	Text      string
	MustCount bool
}

func (t *gType) boundsTokens() []boundsToken {
	switch t.Kind {
	case gInt, gUint:
		lo, hi := implied(t.Bits, t.Kind == gUint)
		one := big.NewInt(1)
		return []boundsToken{
			{new(big.Int).Sub(t.Lo, one).String(), true},
			{new(big.Int).Add(t.Hi, one).String(), true},
			{new(big.Int).Sub(lo, one).String(), true},
			{new(big.Int).Add(hi, one).String(), true},
			{"1e400", true}, {"2.5", true}, {`"7"`, true}, {"null", true},
		}
	case gFixedPoint:
		one := big.NewInt(1)
		return []boundsToken{
			{new(big.Int).Sub(t.Lo, one).String(), true},
			{new(big.Int).Add(t.Hi, one).String(), true},
			{"1e400", true}, {`"7"`, true}, {"null", true},
		}
	case gFloat32:
		return []boundsToken{{"1e400", true}, {"3.5e38", true}, {"2.5", false}, {`"7"`, true}, {"null", true}}
	case gFloat64:
		return []boundsToken{{"1e400", true}, {"2.5", false}, {`"7"`, true}, {"null", true}}
	}
	return nil
}
