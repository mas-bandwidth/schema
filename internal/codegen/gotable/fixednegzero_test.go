package gotable

import "testing"

// TestFixedCompressedNegZeroWrite pins the write side of the compressed-float
// special-values rule (docs/SPEC-TABLES.md §3.3, §3.4): a compressed float's
// -0 is written as +0, the same bytes +0 writes, so every value has one
// encoding. A PLAIN float's -0 is written bit-exact, its sign kept. A
// compressed float is a float32 (SPEC §4.3), so float64 appears only plain.
func TestFixedCompressedNegZeroWrite(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe

fixed table Z
{
    c32 float32 | min = -1, max = 1, resolution = 0.01
    p32 float32
    p64 float64
}
`, `package probe

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestFixedCompressedNegZeroWrite(t *testing.T) {
	t.Parallel()
	negZero32 := float32(math.Copysign(0, -1))
	negZero64 := math.Copysign(0, -1)
	var z Z
	ZReset(&z)
	z.C32 = negZero32
	z.P32 = negZero32
	z.P64 = negZero64
	buf := make([]byte, ZFixedMeasure(1))
	if n := ZFixedSave([]Z{z}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d of %d", n, len(buf))
	}
	body := buf[len(buf)-16:]
	for i, c := range body[0:4] {
		if c != 0 {
			t.Fatalf("a compressed float's -0 was not written as +0: byte %d of % x", i, body[0:4])
		}
	}
	if c32 := math.Float32frombits(binary.LittleEndian.Uint32(body[0:4])); c32 != 0 || math.Signbit(float64(c32)) {
		t.Fatalf("a compressed float's -0 was written as %v", c32)
	}
	if p32 := math.Float32frombits(binary.LittleEndian.Uint32(body[4:8])); p32 != 0 || !math.Signbit(float64(p32)) {
		t.Fatalf("a plain float32's -0 lost its sign: % x", body[4:8])
	}
	if p64 := math.Float64frombits(binary.LittleEndian.Uint64(body[8:16])); p64 != 0 || !math.Signbit(p64) {
		t.Fatalf("a plain float64's -0 lost its sign: % x", body[8:16])
	}
}
`)
}
