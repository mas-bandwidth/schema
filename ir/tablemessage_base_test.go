// A RANGED INTEGER'S BASE LIES INSIDE THE ANNOUNCED KIND
// (docs/SPEC-TABLES.md §3.3 SECURITY): a width the kind holds is not enough,
// because a base near the kind's edge names values past it. DecodeShape holds
// both the base and the top of its range, base + 2^bits - 1, to the kind's own
// domain, signed and unsigned, 128-bit included.
package ir

import (
	"math/big"
	"testing"
)

// kindDomain is a kind's own storage domain, exact: the two's complement range
// for a signed kind and [0, 2^width - 1] for an unsigned one.
func kindDomain(kind uint8) (lo, hi *big.Int) {
	width := uint(TableMessageKindBits(kind))
	one := big.NewInt(1)
	hi = new(big.Int).Sub(new(big.Int).Lsh(one, width), one)
	lo = new(big.Int)
	if TableKindSigned(int(kind)) {
		hi.Rsh(hi, 1)
		lo.Neg(new(big.Int).Lsh(one, width-1))
	}
	return lo, hi
}

// highestLawfulBase is the largest base a ranged shape at `bits` can name
// while its top stays inside the kind: hi - (2^bits - 1).
func highestLawfulBase(kind uint8, bits int64) *big.Int {
	_, hi := kindDomain(kind)
	one := big.NewInt(1)
	return new(big.Int).Sub(hi, new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one))
}

func decodeEncodedShape(t *testing.T, kind uint8, shape TableMessageShape) (TableMessageShape, bool) {
	t.Helper()
	encoded := TableVocabularyEntry{Id: 1, Kind: kind, Shape: shape}.Encode(nil)
	got, _, ok := DecodeShape(encoded[9:], kind)
	return got, ok
}

func TestDecodeShapeRangedBaseInsideKind(t *testing.T) {
	t.Parallel()

	kinds := []uint8{
		TableKindI8,
		TableKindU8,
		TableKindI32,
		TableKindU64,
		TableKindI128,
	}
	for _, kind := range kinds {
		bits := TableMessageKindBits(kind)
		lawful := highestLawfulBase(kind, bits)
		past := new(big.Int).Add(lawful, big.NewInt(1))

		t.Run("lawful edge", func(t *testing.T) {
			t.Parallel()
			got, ok := decodeEncodedShape(t, kind, TableMessageShape{
				Packing: TableMessageRanged,
				Bits:    bits,
				Base:    lawful,
			})
			if !ok {
				t.Fatalf("kind %d: DecodeShape refused the highest lawful base %s at %d bits", kind, lawful, bits)
			}
			if got.Base == nil || got.Base.Cmp(lawful) != 0 {
				t.Errorf("kind %d: DecodeShape base = %v, want %s", kind, got.Base, lawful)
			}
			if got.Bits != bits {
				t.Errorf("kind %d: DecodeShape bits = %d, want %d", kind, got.Bits, bits)
			}
		})

		t.Run("one past", func(t *testing.T) {
			t.Parallel()
			if _, ok := decodeEncodedShape(t, kind, TableMessageShape{
				Packing: TableMessageRanged,
				Bits:    bits,
				Base:    past,
			}); ok {
				t.Errorf("kind %d: DecodeShape accepted base %s at %d bits, whose top is past the kind", kind, past, bits)
			}
		})
	}

	t.Run("negative base for an unsigned kind", func(t *testing.T) {
		t.Parallel()
		rows := []struct {
			name string
			kind uint8
			bits int64
			base *big.Int
		}{
			{"uint8", TableKindU8, 8, big.NewInt(-1)},
			{"uint128", TableKindU128, 8, big.NewInt(-1)},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				t.Parallel()
				if _, ok := decodeEncodedShape(t, row.kind, TableMessageShape{
					Packing: TableMessageRanged,
					Bits:    row.bits,
					Base:    row.base,
				}); ok {
					t.Errorf("kind %d: DecodeShape accepted the negative base %s", row.kind, row.base)
				}
			})
		}
	})

	t.Run("a base itself past the kind", func(t *testing.T) {
		t.Parallel()
		if _, ok := decodeEncodedShape(t, TableKindI32, TableMessageShape{
			Packing: TableMessageRanged,
			Bits:    1,
			Base:    new(big.Int).Lsh(big.NewInt(1), 40),
		}); ok {
			t.Error("DecodeShape accepted an int32 base near 2^40")
		}
	})

	t.Run("unsigned 128-bit high base", func(t *testing.T) {
		t.Parallel()
		high := new(big.Int).Lsh(big.NewInt(1), 127)
		got, ok := decodeEncodedShape(t, TableKindU128, TableMessageShape{
			Packing: TableMessageRanged,
			Bits:    1,
			Base:    high,
		})
		if !ok {
			t.Fatalf("DecodeShape refused the lawful uint128 base %s", high)
		}
		if got.Base == nil || got.Base.Cmp(high) != 0 {
			t.Errorf("DecodeShape base = %v, want %s", got.Base, high)
		}
		_, hi := kindDomain(TableKindU128)
		if _, ok := decodeEncodedShape(t, TableKindU128, TableMessageShape{
			Packing: TableMessageRanged,
			Bits:    1,
			Base:    hi,
		}); ok {
			t.Errorf("DecodeShape accepted the uint128 base %s at 1 bit, whose top is past the kind", hi)
		}
	})
}
