package gotable

import (
	"regexp"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 IN THE VARIABLE FORM TOO
// (docs/SPEC-TABLES.md §3.3: "the writer stores +0 for a compressed float's
// -0, the same bits it stores for +0"; ruling fixed-compressed-negzero). Under
// a zero default a -0 is elided (== compares it equal); wherever it rides (a
// non-zero default, an array element) the variable writer stored its raw bits,
// 0x80000000, where the fixed writer already stored +0. The plain float is the
// control: its -0 is stored bit for bit.
const negZeroStoreSchema = `package probe
table Probe {
 half float32 = 0.5 | min = -1, max = 1, resolution = 0.01
 many [3]float32 | min = -1, max = 1, resolution = 0.01
 plain float32 = 0.5
}
`

var float32PutRE = regexp.MustCompile(`\.Put32\(math\.Float32bits\(([^()]+)\)\)`)

// TestCompressedFloatVariableWriteFoldsNegativeZero is the fast half: every
// variable-form put of a compressed float sits in the zero test's else arm, so
// its -0 stores the +0 bits; a plain float's put has no such test.
func TestCompressedFloatVariableWriteFoldsNegativeZero(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	for _, data := range generate(t, negZeroStoreSchema) {
		src.Write(data)
	}
	text := src.String()
	compressed, plain := 0, 0
	for _, at := range float32PutRE.FindAllStringSubmatchIndex(text, -1) {
		expr := text[at[2]:at[3]]
		before := text[max(0, at[0]-200):at[0]]
		folded := strings.Contains(before, "if "+expr+" == 0 {") && strings.Contains(before, ".Put32(0)")
		switch {
		case strings.Contains(expr, "Half") || strings.Contains(expr, "Many"):
			compressed++
			if !folded {
				t.Errorf("a compressed float is put raw, so its -0 stores 0x80000000: %q", text[max(0, at[0]-120):at[1]])
			}
		case strings.Contains(expr, "Plain"):
			plain++
			if folded {
				t.Errorf("a plain float's -0 is folded to +0; it rides bit for bit: %q", text[max(0, at[0]-120):at[1]])
			}
		}
	}
	if compressed == 0 || plain == 0 {
		t.Fatalf("found %d compressed and %d plain float32 puts; the probe sees neither shape", compressed, plain)
	}
}

// TestCompressedFloatVariableWriteNegativeZeroBytes is the run half (behind the
// slow gate): the generated writer's bytes for -0 equal its bytes for +0.
func TestCompressedFloatVariableWriteNegativeZeroBytes(t *testing.T) {
	runGenerated(t, negZeroStoreSchema, negZeroStoreProbe)
}

const negZeroStoreProbe = `package probe

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func probeBytes(t *testing.T, zero float32) []byte {
	t.Helper()
	v := &Probe{}
	v.Half = zero
	v.Many = [3]float32{zero, 0.5, zero}
	v.Plain = float32(math.Copysign(0, -1))
	buffer := make([]byte, 256)
	var ids TableIds
	w := TableWriter{Buffer: buffer, Ids: &ids}
	w.Put8(1)
	if !ProbeSaveBody(&w, v) {
		t.Fatal("save")
	}
	w.Trailer()
	if w.Overflow || ids.Overflow {
		t.Fatal("overflow")
	}
	return buffer[:w.Offset]
}

func TestNegativeZeroStoresPlusZero(t *testing.T) {
	minus := probeBytes(t, float32(math.Copysign(0, -1)))
	plus := probeBytes(t, 0)
	if !bytes.Equal(minus, plus) {
		t.Fatalf("-0 wrote other bytes than +0:\n-0 %x\n+0 %x", minus, plus)
	}
	if n := bytes.Count(minus, binary.LittleEndian.AppendUint32(nil, 0x80000000)); n != 1 {
		t.Fatalf("%d copies of the -0 bits, want 1 (the plain float's): %x", n, minus)
	}
}
`
