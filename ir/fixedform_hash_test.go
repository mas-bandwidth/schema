package ir

import (
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
