package ir

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/big"
	"testing"
)

func digestV2Root(fields ...*Field) *Struct {
	return &Struct{Name: "Root", Fields: fields, IsTable: true, FixedDeclared: true}
}

func digestV2Int(name string, lo, hi *big.Int, signed bool) *Field {
	return &Field{Name: name, Type: FieldType{Kind: TInt, Width: 128, Signed: signed}, HasIntRange: true, IntMin: lo, IntMax: hi}
}

func digestV2Identity(st *Struct) uint64 {
	return TableFixedLayoutHash(TableFixedLayoutBytes(TableFixedWalkRoot(st)), st)
}

func TestTableFixedDigestV2AllRootsHaveEpoch(t *testing.T) {
	root := digestV2Root(&Field{Name: "x", Type: FieldType{Kind: TInt, Width: 32}})
	if got := TableFixedDefinitionsDigest(root); string(got) != "\x00schema-fixed-definitions\x02" {
		t.Fatalf("plain root epoch = %x", got)
	}
	layout := TableFixedLayoutBytes(TableFixedWalkRoot(root))
	legacy := TableFixedLayoutHashRecorded(layout, TableFixedDefinitionsDigestLegacy(root))
	if legacy == digestV2Identity(root) {
		t.Fatal("new plain root reused a legacy layout-only key")
	}
	if TableFixedLayoutHash(layout, nil) != legacy {
		t.Fatal("nil-schema compatibility operation changed historical keys")
	}
}

func TestTableFixedDigestV2CanonicalRowPin(t *testing.T) {
	root := digestV2Root(&Field{Name: "x", Type: FieldType{Kind: TBits, Width: 13}})
	// Literal protocol bytes: prefix, B row, two wire-address path segments,
	// four-byte payload. This pins framing independently of the encoder helpers.
	want, err := hex.DecodeString("00736368656d612d66697865642d646566696e6974696f6e7302421a000000740d000000a5b251ef2bfe7cca6607000000071702864cf563af040000000d000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := TableFixedDefinitionsDigest(root); !bytes.Equal(got, want) {
		t.Fatalf("v2 row bytes = %x; want %x", got, want)
	}
}

func digestV2RowCount(t *testing.T, digest []byte, tag byte) int {
	t.Helper()
	if !bytes.HasPrefix(digest, []byte(tableFixedDigestV2Prefix)) {
		t.Fatal("missing v2 prefix")
	}
	n := 0
	for p := len(tableFixedDigestV2Prefix); p < len(digest); {
		if digest[p] == tag {
			n++
		}
		p++
		for range 2 { // separately framed path and payload
			if len(digest)-p < 4 {
				t.Fatal("truncated row length")
			}
			size := int(binary.LittleEndian.Uint32(digest[p:]))
			p += 4
			if size > len(digest)-p {
				t.Fatal("row length exceeds digest")
			}
			p += size
		}
	}
	return n
}

func TestTableFixedDigestV2WideBoundsDistinguishLegacyCollision(t *testing.T) {
	pow := func(n uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), n) }
	for _, signed := range []bool{false, true} {
		a := digestV2Root(digestV2Int("x", big.NewInt(0), pow(80), signed))
		b := digestV2Root(digestV2Int("x", big.NewInt(0), pow(81), signed))
		if !bytes.Equal(TableFixedDefinitionsDigestLegacy(a), TableFixedDefinitionsDigestLegacy(b)) {
			t.Fatal("fixture no longer demonstrates the legacy collision")
		}
		if digestV2Identity(a) == digestV2Identity(b) {
			t.Fatal("wide bound change retained the writer identity")
		}
	}
	values := []*big.Int{
		new(big.Int).Neg(pow(127)), new(big.Int).Neg(pow(80)), big.NewInt(-1), big.NewInt(0),
		big.NewInt(1), pow(80), new(big.Int).Sub(pow(127), big.NewInt(1)), new(big.Int).Sub(pow(128), big.NewInt(1)),
	}
	seen := map[string]bool{}
	for _, n := range values {
		encoded := string(tableFixedDigestInteger(nil, n))
		if seen[encoded] {
			t.Fatalf("lossy canonical integer at %s", n)
		}
		seen[encoded] = true
	}
	if bytes.Equal(tableFixedDigestInteger(nil, nil), tableFixedDigestInteger(nil, big.NewInt(0))) {
		t.Fatal("invalid missing bound aliases zero")
	}
}

func TestTableFixedDigestV2RangeAttributionAndAliases(t *testing.T) {
	root := func(bounded string) *Struct {
		a, b := digestV2Int("a", big.NewInt(0), big.NewInt(10), true), digestV2Int("b", big.NewInt(0), big.NewInt(10), true)
		a.HasIntRange, b.HasIntRange = bounded == "a", bounded == "b"
		return digestV2Root(a, b)
	}
	a, b := root("a"), root("b")
	if !bytes.Equal(TableFixedDefinitionsDigestLegacy(a), TableFixedDefinitionsDigestLegacy(b)) || !bytes.Equal(TableFixedLayoutBytes(TableFixedWalkRoot(a)), TableFixedLayoutBytes(TableFixedWalkRoot(b))) {
		t.Fatal("fixture must have equal legacy digest and layout")
	}
	if digestV2Identity(a) == digestV2Identity(b) {
		t.Fatal("anonymous range moved to a different field without moving identity")
	}
	before := digestV2Identity(a)
	a.Fields[0].Name, a.Fields[0].WasName = "renamed", "a"
	if digestV2Identity(a) != before {
		t.Fatal("was alias changed the wire-addressed identity")
	}
}

func TestTableFixedDigestV2CountMinimaAndNestedPaths(t *testing.T) {
	leaf := &Struct{Name: "Leaf", Fields: []*Field{digestV2Int("x", big.NewInt(0), new(big.Int).Lsh(big.NewInt(1), 80), false)}}
	a := &Field{Name: "a", Type: FieldType{Kind: TNamed, Name: "Leaf", Ref: leaf, Optional: true}, Array: ArrayCounted, ArrayBound: 4}
	b := &Field{Name: "b", Type: FieldType{Kind: TNamed, Name: "Leaf", Ref: leaf}, Array: ArrayCounted, ArrayBound: 4}
	root := digestV2Root(a, b)
	before, legacy := digestV2Identity(root), TableFixedDefinitionsDigestLegacy(root)
	b.ArrayMin = 1
	if digestV2Identity(root) == before || !bytes.Equal(legacy, TableFixedDefinitionsDigestLegacy(root)) {
		t.Fatal("count minimum must move only the prospective definition identity")
	}
	b.ArrayMin = 0
	// A shared named type is reached twice. Removing the first occurrence must
	// leave the second occurrence's bound explicitly attributed to its own path.
	one := TableFixedDefinitionsDigest(digestV2Root(b))
	two := TableFixedDefinitionsDigest(root)
	if digestV2RowCount(t, two, 'R') != 2 {
		t.Fatal("each occurrence of a shared definition needs its own bound row")
	}
	if bytes.Equal(one, two) {
		t.Fatal("shared definition suppressed a wire occurrence")
	}
	un := &Union{Name: "Choice", Max: 2, Variants: []UnionVariant{{Name: "a", F: digestV2Int("a", big.NewInt(0), big.NewInt(8), true)}, {Name: "b", F: digestV2Int("b", big.NewInt(0), big.NewInt(8), true)}}}
	u := digestV2Root(&Field{Name: "u", Type: FieldType{Kind: TNamed, Name: "Choice", Ref: un}})
	un.Variants[1].F.HasIntRange = false
	first := digestV2Identity(u)
	un.Variants[0].F.HasIntRange, un.Variants[1].F.HasIntRange = false, true
	if digestV2Identity(u) == first {
		t.Fatal("union arm path did not attribute its range")
	}
}

func TestTableFixedDigestV2FlagsAndBitsFacts(t *testing.T) {
	flags := &Flags{Name: "Flags", WireBits: 8, Variants: []string{"A", "B"}}
	f := &Field{Name: "f", Type: FieldType{Kind: TNamed, Name: "Flags", Ref: flags}}
	b := &Field{Name: "b", Type: FieldType{Kind: TBits, Width: 13}}
	r := digestV2Root(f, b)
	before := digestV2Identity(r)
	flags.Variants[0], flags.Variants[1] = flags.Variants[1], flags.Variants[0]
	if digestV2Identity(r) == before {
		t.Fatal("flags assignments omitted")
	}
	before = digestV2Identity(r)
	b.Type.Width = 14 // same u16 layout, distinct validity constraint
	if digestV2Identity(r) == before {
		t.Fatal("bits width omitted")
	}
	before = digestV2Identity(r)
	flags.Name, f.Type.Name = "RenamedFlags", "RenamedFlags"
	if digestV2Identity(r) != before {
		t.Fatal("mutable declaration spelling entered identity")
	}
}

func TestTableFixedDigestV2FloatAndFixedFacts(t *testing.T) {
	f := &Field{Name: "x", Type: FieldType{Kind: TFloat64}, HasFloatRange: true, FMin: 0.25, FMax: 0.5, Resolution: 0.01}
	r := digestV2Root(f)
	before := digestV2Identity(r)
	f.Resolution = 0.001
	if digestV2Identity(r) == before {
		t.Fatal("float resolution omitted")
	}
	before = digestV2Identity(r)
	f.FMin = math.Nextafter(f.FMin, 1)
	if digestV2Identity(r) == before {
		t.Fatal("float bound lost precision")
	}
	x := digestV2Int("raw", big.NewInt(0), new(big.Int).Lsh(big.NewInt(1), 90), true)
	x.Type = FieldType{Kind: TFixed, Width: 128, IntBits: 96, FracBits: 32, Signed: true}
	r = digestV2Root(x)
	before = digestV2Identity(r)
	x.IntMax = new(big.Int).Lsh(big.NewInt(1), 91)
	if digestV2Identity(r) == before {
		t.Fatal("fixed raw bound truncated")
	}
	before = digestV2Identity(r)
	x.Type.IntBits, x.Type.FracBits = 95, 33
	if digestV2Identity(r) == before {
		t.Fatal("fixed scale omitted")
	}
}

func TestTableFixedDigestV2RecordedSchemeSelection(t *testing.T) {
	r := digestV2Root(digestV2Int("x", big.NewInt(0), big.NewInt(100), true))
	for _, recorded := range [][]byte{TableFixedDefinitionsDigestLegacy(r), TableFixedDefinitionsDigest(r)} {
		got, err := TableFixedDefinitionsDigestForRecorded(r, recorded)
		if err != nil || !bytes.Equal(got, recorded) {
			t.Fatalf("recorded scheme validation = %x, %v; want %x", got, err, recorded)
		}
	}
	got, err := TableFixedDefinitionsDigestForRecorded(r, nil)
	if err != nil || len(got) == 0 {
		t.Fatal("empty recorded key silently erased source constraints")
	}
	for _, unknown := range [][]byte{[]byte("\x00schema-fixed-definitions\x03"), []byte("\x00schema-fixed-definitions"), []byte("?")} {
		if _, err := TableFixedDefinitionsDigestForRecorded(r, unknown); err == nil {
			t.Fatalf("unknown scheme accepted: %x", unknown)
		}
	}
}
