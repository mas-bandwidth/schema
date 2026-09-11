package ir

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/big"
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

func float64LE(v float64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
	return buf[:]
}
