package ir

import (
	"bytes"
	"encoding/binary"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
)

func layoutOnlyHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func TestTableFixedLayoutHashEmptyDigestEqualsLayoutBytes(t *testing.T) {
	layout := []byte{0x01, 0x02, 0x03, 0x04}
	plain := &Struct{Name: "Plain", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TInt, Width: 32}},
	}}
	got := TableFixedLayoutHash(layout, plain)
	want := layoutOnlyHash(layout)
	if got != want {
		t.Fatalf("empty digest hash = 0x%016x, layout-bytes-only = 0x%016x", got, want)
	}
	if TableFixedLayoutHash(layout, nil) != want {
		t.Fatal("a nil schema is an empty digest and must not move the hash")
	}
	if len(TableFixedDefinitionsDigest(plain)) != 0 {
		t.Fatalf("plain int32 digest = %q, want empty", TableFixedDefinitionsDigest(plain))
	}
}

func TestTableFixedLayoutHashRangeMovesTheHash(t *testing.T) {
	layout := []byte{0x01, 0x02, 0x03, 0x04}
	ranged := &Struct{Name: "Ranged", Fields: []*Field{
		{
			Name:        "n",
			Type:        FieldType{Kind: TInt, Width: 32},
			HasIntRange: true,
			IntMin:      big.NewInt(0),
			IntMax:      big.NewInt(100),
		},
	}}
	got := TableFixedLayoutHash(layout, ranged)
	if got == layoutOnlyHash(layout) {
		t.Fatal("a range must fold into the hash; digest is not optional")
	}
	if len(TableFixedDefinitionsDigest(ranged)) == 0 {
		t.Fatal("a range must produce a nonempty digest")
	}
}

func TestTableFixedDefinitionsDigestFloatRangeIsR(t *testing.T) {
	layout := []byte{0x01, 0x02, 0x03, 0x04}
	a := &Struct{Name: "F", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0, FMax: 10},
	}}
	b := &Struct{Name: "F", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0, FMax: 20},
	}}
	da := TableFixedDefinitionsDigest(a)
	db := TableFixedDefinitionsDigest(b)
	if len(da) == 0 || da[0] != 'R' {
		t.Fatalf("a float range must emit 'R', got %q", da)
	}
	if bytes.Equal(da, db) {
		t.Fatal("two float ranges that differ must not share a digest")
	}
	if TableFixedLayoutHash(layout, a) == TableFixedLayoutHash(layout, b) {
		t.Fatal("a float range must move the wire hash; otherwise an old reader cannot refuse the widening")
	}
	want := append([]byte{'R'}, float64LE(0)...)
	want = append(want, float64LE(10)...)
	if !bytes.Equal(da, want) {
		t.Fatalf("float range bytes: got %x, want %x (IEEE-754 bits, i64 LE)", da, want)
	}
}

func TestTableFixedDefinitionsDigestFlagsOnceByName(t *testing.T) {
	fl := &Flags{Name: "Perks", Variants: []string{"Shielded", "Cloaked"}, WireBits: 2}
	st := &Struct{Name: "Root", Fields: []*Field{
		{Name: "a", Type: FieldType{Kind: TNamed, Name: "Perks", Ref: fl}},
		{Name: "b", Type: FieldType{Kind: TNamed, Name: "Perks", Ref: fl}},
	}}
	d := TableFixedDefinitionsDigest(st)
	n := 0
	for _, c := range d {
		if c == 'F' {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a flags type is a named type and emits once, got %d 'F' in %q", n, d)
	}
}

func TestTableFixedDefinitionsDigestUnionOnceByName(t *testing.T) {
	inner := &Struct{Name: "Leaf", Fields: []*Field{
		{Name: "n", Type: FieldType{Kind: TInt, Width: 32}, HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(4)},
	}}
	un := &Union{Name: "Pick", Variants: []UnionVariant{
		{Name: "a", F: &Field{Name: "a", Type: FieldType{Kind: TNamed, Name: "Leaf", Ref: inner}}},
	}}
	st := &Struct{Name: "Root", Fields: []*Field{
		{Name: "p", Type: FieldType{Kind: TNamed, Name: "Pick", Ref: un}},
		{Name: "q", Type: FieldType{Kind: TNamed, Name: "Pick", Ref: un}},
	}}
	d := TableFixedDefinitionsDigest(st)
	n := 0
	for _, c := range d {
		if c == 'R' {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a union is a named type and its payload range emits once, got %d 'R' in %q", n, d)
	}
}

func TestTableFixedDefinitionsDigestSeenIsOneMapByBareName(t *testing.T) {
	fl := &Flags{Name: "Same", Variants: []string{"Shielded"}, WireBits: 1}
	inner := &Struct{Name: "Same", Fields: []*Field{
		{Name: "n", Type: FieldType{Kind: TInt, Width: 32}, HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(4)},
	}}
	un := &Union{Name: "Same", Variants: []UnionVariant{
		{Name: "a", F: &Field{Name: "a", Type: FieldType{Kind: TInt, Width: 32}, HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(8)}},
	}}
	st := &Struct{Name: "Root", Fields: []*Field{
		{Name: "f", Type: FieldType{Kind: TNamed, Name: "Same", Ref: fl}},
		{Name: "s", Type: FieldType{Kind: TNamed, Name: "Same", Ref: inner}},
		{Name: "u", Type: FieldType{Kind: TNamed, Name: "Same", Ref: un}},
	}}
	d := TableFixedDefinitionsDigest(st)
	nF, nR := 0, 0
	for _, c := range d {
		if c == 'F' {
			nF++
		}
		if c == 'R' {
			nR++
		}
	}
	if nF != 1 {
		t.Fatalf("flags emit once by bare name, got %d 'F' in %q", nF, d)
	}
	if nR != 0 {
		t.Fatalf("structs, flags and unions share one seen map keyed by bare name; after flags Same, the struct and union of that name must not emit, got %d 'R' in %q", nR, d)
	}
}

func TestTableFixedDefinitionsDigestLReservedUntilALimitExists(t *testing.T) {
	hasLimit := irTypeHasLimitField(reflect.TypeFor[Field]()) ||
		irTypeHasLimitField(reflect.TypeFor[Struct]()) ||
		irTypeHasLimitField(reflect.TypeFor[Unit]())
	emitsL := digestSourceEmitsL(t)
	switch {
	case hasLimit && !emitsL:
		t.Fatal("a table-level reader-side limit exists; TableFixedDefinitionsDigest must emit 'L' + u64 LE")
	case !hasLimit && emitsL:
		t.Fatal("'L' is reserved until a table-level limit exists; the row stays empty")
	}
	if hasLimit {
		return
	}
	st := &Struct{Name: "Plain", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TInt, Width: 32}},
	}}
	if bytes.IndexByte(TableFixedDefinitionsDigest(st), 'L') >= 0 {
		t.Fatal("'L' is reserved; a table with no reader-side limit must not emit it")
	}
}

func irTypeHasLimitField(rt reflect.Type) bool {
	for sf := range rt.Fields() {
		if strings.Contains(strings.ToLower(sf.Name), "limit") {
			return true
		}
	}
	return false
}

func digestSourceEmitsL(t *testing.T) bool {
	t.Helper()
	src, err := os.ReadFile("fixedform.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixedform.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var emits bool
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "append" {
			return true
		}
		for _, a := range call.Args {
			bl, ok := a.(*ast.BasicLit)
			if !ok || bl.Kind != token.CHAR {
				continue
			}
			if bl.Value == "'L'" {
				emits = true
			}
		}
		return true
	})
	return emits
}

func float64LE(v float64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
	return buf[:]
}

// TestTableFixedDefinitionsDigestFloatBoundIsItsFloat64Bits pins ONE float
// range's digest bytes against a LITERAL. The contract table in
// docs/FIXED-FORM-ALGORITHM.md §5.2 used to say min and max ride "each as an
// i64 LE" for every range, integer and float alike; the code writes
// math.Float64bits for a float bound, and a reader that followed the words
// would hash 0.5 and 0.25 the same — both truncate to the i64 `0`. The literal
// below is 0.5 and 0.25 as their IEEE-754 float64 bits, little-endian.
func TestTableFixedDefinitionsDigestFloatBoundIsItsFloat64Bits(t *testing.T) {
	st := &Struct{Name: "F", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0.25, FMax: 0.5},
	}}
	want := []byte{
		'R',
		0, 0, 0, 0, 0, 0, 0xd0, 0x3f, // 0.25
		0, 0, 0, 0, 0, 0, 0xe0, 0x3f, // 0.5
	}
	if got := TableFixedDefinitionsDigest(st); !bytes.Equal(got, want) {
		t.Fatalf("a float bound rides as its float64 bits: got %x, want %x", got, want)
	}
	// And the same two bounds truncated to i64 are BOTH zero, which is the read
	// the retired wording invited: it must not be the read the code takes.
	other := &Struct{Name: "F", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0.75, FMax: 0.9},
	}}
	if bytes.Equal(TableFixedDefinitionsDigest(st), TableFixedDefinitionsDigest(other)) {
		t.Fatal("two float ranges inside the same integer truncation must not share a digest")
	}
}
