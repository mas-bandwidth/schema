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
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0, FMax: 10, Resolution: 0.01},
	}}
	b := &Struct{Name: "F", Fields: []*Field{
		{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0, FMax: 20, Resolution: 0.01},
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
	want = append(want, 'Q')
	want = append(want, float64LE(0.01)...)
	if !bytes.Equal(da, want) {
		t.Fatalf("float range bytes: got %x, want %x ('R' min max, then 'Q' resolution, IEEE-754 bits LE)", da, want)
	}
}

// THE RESOLUTION IS IN THE DIGEST, ALONE. A compressed float rides as the
// float32 in the fixed form, so the step is nowhere in the layout bytes: if it
// did not move the hash, a finer reader could not refuse a coarsened endpoint by
// hash, which is the whole mechanism (bill §2, §13).
func TestTableFixedDefinitionsDigestResolutionMovesTheHash(t *testing.T) {
	layout := []byte{0x01, 0x02, 0x03, 0x04}
	field := func(res float64) *Struct {
		return &Struct{Name: "F", Fields: []*Field{
			{Name: "x", Type: FieldType{Kind: TFloat32}, HasFloatRange: true, FMin: 0, FMax: 1, Resolution: res},
		}}
	}
	fine, coarse := field(0.01), field(0.1)
	if bytes.Equal(TableFixedDefinitionsDigest(fine), TableFixedDefinitionsDigest(coarse)) {
		t.Fatal("two resolutions over the same bounds must not share a digest")
	}
	if TableFixedLayoutHash(layout, fine) == TableFixedLayoutHash(layout, coarse) {
		t.Fatal("a moved resolution must move the wire hash; otherwise a coarsened endpoint cannot be refused")
	}
	d := TableFixedDefinitionsDigest(fine)
	if n := bytes.Count(d, []byte{'Q'}); n != 1 {
		t.Fatalf("one compressed float emits one 'Q', got %d in %x", n, d)
	}
}

func TestTableFixedDefinitionsDigestFlagsOnceByName(t *testing.T) {
	t.Parallel()
	fl := &Flags{Name: "Perks", Variants: []string{"Shielded", "Cloaked"}, WireBits: 2}
	st := &Struct{Name: "Root", Fields: []*Field{
		{Name: "a", Type: FieldType{Kind: TNamed, Name: "Perks", Ref: fl}},
		{Name: "b", Type: FieldType{Kind: TNamed, Name: "Perks", Ref: fl}},
	}}
	d := TableFixedDefinitionsDigest(st)
	n, g := 0, 0
	for _, c := range d {
		switch c {
		case 'F':
			n++
		case 'G':
			g++
		}
	}
	if n != 1 {
		t.Fatalf("a flags type is a named type and its definitions emit once, got %d 'F' in %q", n, d)
	}
	if g != 1 {
		t.Fatalf("the later field naming that flags type carries one reference row, got %d 'G' in %q", g, d)
	}
}

// A RANGE BOUND OUTSIDE i64 is carried whole. The old i64 row saturated such a
// bound to zero, so two different wide maxima hashed alike and a reader could
// not refuse the widening. The wide row keeps both bounds as full sixteen-byte
// two's-complement values, and an in-i64 range keeps its old 'R' bytes.
func TestTableFixedDefinitionsDigestWideBoundsAreDistinct(t *testing.T) {
	t.Parallel()
	wide := func(max *big.Int) *Struct {
		return &Struct{Name: "Wide", Fields: []*Field{{
			Name:        "n",
			Type:        FieldType{Kind: TInt, Width: 64, Signed: false},
			HasIntRange: true,
			IntMin:      big.NewInt(0),
			IntMax:      max,
		}}}
	}
	top := new(big.Int).SetUint64(math.MaxUint64)
	next := new(big.Int).Sub(top, big.NewInt(1))
	da := TableFixedDefinitionsDigest(wide(top))
	db := TableFixedDefinitionsDigest(wide(next))
	if len(da) != 33 || da[0] != 'W' {
		t.Fatalf("a wide range digest = % x, want 'W' plus two sixteen-byte bounds", da)
	}
	if bytes.Equal(da, db) {
		t.Fatalf("two wide maxima must not share a digest: both % x", da)
	}
	layout := []byte{0x01, 0x02, 0x03, 0x04}
	if TableFixedLayoutHash(layout, wide(top)) == TableFixedLayoutHash(layout, wide(next)) {
		t.Fatal("two wide maxima must not share the wire hash; otherwise a reader cannot refuse the widening")
	}

	inRange := &Struct{Name: "Narrow", Fields: []*Field{{
		Name:        "n",
		Type:        FieldType{Kind: TInt, Width: 32},
		HasIntRange: true,
		IntMin:      big.NewInt(-5),
		IntMax:      big.NewInt(5),
	}}}
	want := append([]byte{'R'}, int64LE(-5)...)
	want = append(want, int64LE(5)...)
	if got := TableFixedDefinitionsDigest(inRange); !bytes.Equal(got, want) {
		t.Fatalf("an in-i64 range keeps its 'R' row: got % x, want % x", got, want)
	}
}

// A FLAGS TYPE NAMED BY MORE THAN ONE FIELD carries a reference row at each
// later field. Without it, one use and two uses of the same flags type hashed
// alike, which is the collision the old by-name dedup left.
func TestTableFixedDefinitionsDigestRepeatedFlagsReference(t *testing.T) {
	t.Parallel()
	fl := &Flags{Name: "Perks", Variants: []string{"Shielded", "Cloaked"}, WireBits: 2}
	field := func(name string) *Field {
		return &Field{Name: name, Type: FieldType{Kind: TNamed, Name: "Perks", Ref: fl}}
	}
	one := &Struct{Name: "Root", Fields: []*Field{field("a")}}
	twice := &Struct{Name: "Root", Fields: []*Field{field("a"), field("b")}}
	thrice := &Struct{Name: "Root", Fields: []*Field{field("a"), field("b"), field("c")}}
	dOne := TableFixedDefinitionsDigest(one)
	dTwo := TableFixedDefinitionsDigest(twice)
	dThree := TableFixedDefinitionsDigest(thrice)
	if bytes.Equal(dOne, dTwo) {
		t.Fatalf("one use and two uses of a flags type must not share a digest: both % x", dOne)
	}
	if !bytes.HasPrefix(dTwo, dOne) {
		t.Fatalf("a later use must leave the first use's bytes alone: once % x, twice % x", dOne, dTwo)
	}
	for name, d := range map[string][]byte{"twice": dTwo, "thrice": dThree} {
		if n := bytes.Count(d, []byte{'F'}); n != 1 {
			t.Fatalf("%s: a flags type emits its definition once, got %d 'F' in % x", name, n, d)
		}
	}
	if n := bytes.Count(dTwo, []byte{'G'}); n != 1 {
		t.Fatalf("twice: the second use carries one reference row, got %d 'G' in % x", n, dTwo)
	}
	if n := bytes.Count(dThree, []byte{'G'}); n != 2 {
		t.Fatalf("thrice: each later use carries a reference row, got %d 'G' in % x", n, dThree)
	}
	if dTwo[len(dTwo)-1] != 'G' {
		t.Fatalf("twice: the digest ends in the second use's reference row, got % x", dTwo)
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

func int64LE(v int64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	return buf[:]
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
		// 'Q' rides beside every float range's 'R' — the RESOLUTION row, the
		// step as an f64's bits. This field is not a compressed float, so the
		// step is 0.0 and the row is eight zero bytes; the row is here all the
		// same, because the digest's shape does not branch on whether a float
		// happens to be compressed.
		'Q',
		0, 0, 0, 0, 0, 0, 0, 0, // resolution 0.0
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
