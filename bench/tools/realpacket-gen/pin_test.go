// The pin gate: bench/corpus/RealWorld.schema is a golden, generated once and
// never regenerated (BENCH-STANDARD §1.7). This test does NOT regenerate it —
// it proves three things about the pin as checked in:
//
//  1. determinism: the recorded generator + seed + field count reproduce the
//     pinned file byte-for-byte, so the header's provenance line is true;
//  2. the header's numbers are the generator's numbers (bits, bytes, fields);
//  3. the arithmetic agrees with the compiler: the pinned all-defaults wire
//     bits plus the untaken branch bodies equal ir.MaxBitsStruct over the
//     checked unit — the same width formulas every backend advertises.
//
// If this test fails, the pin did not drift — someone edited the pinned file
// or this generator. The fix is a NEW corpus file under a NEW name, never a
// regeneration in place.
package main

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	pinPath   = "../../corpus/RealWorld.schema"
	pinSeed   = 20260816
	pinFields = 95
	pinBits   = 1629
	pinDecls  = 97
)

func TestPinReproduces(t *testing.T) {
	want, err := os.ReadFile(pinPath)
	if err != nil {
		t.Fatalf("reading the pin: %v", err)
	}
	got, st, err := generate(pinSeed, pinFields)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generator output differs from the pinned file — the pin or the generator was edited; a corpus change means a NEW file under a NEW name")
	}
	if st.totalBits != pinBits {
		t.Fatalf("pinned wire bits: generator says %d, the pin records %d", st.totalBits, pinBits)
	}
	if st.declCount != pinDecls {
		t.Fatalf("field declarations: generator says %d, the pin records %d", st.declCount, pinDecls)
	}
	if len(st.gates) != 4 {
		t.Fatalf("expected exactly 4 branch gates, got %d", len(st.gates))
	}
}

func TestPinAgreesWithCompilerWidths(t *testing.T) {
	src, err := os.ReadFile(pinPath)
	if err != nil {
		t.Fatalf("reading the pin: %v", err)
	}
	name := filepath.Base(pinPath)
	ast, perrs := parser.Parse(name, src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{
		Path:  pinPath,
		Name:  name,
		Base:  strings.TrimSuffix(name, ".schema"),
		Bytes: src,
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	_, st, err := generate(pinSeed, pinFields)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var found bool
	for _, f := range unit.Files {
		for _, d := range f.Decls {
			stct, ok := d.(*ir.Struct)
			if !ok || stct.Name != "RealPacket" {
				continue
			}
			found = true
			// MaxBitsStruct takes every branch's larger side, so it equals the
			// pinned wire plus the bodies under false gates.
			want := st.totalBits + st.untakenBits
			if got := ir.MaxBitsStruct(stct); got != want {
				t.Fatalf("ir.MaxBitsStruct(RealPacket) = %d, want %d (pinned %d + untaken %d)",
					got, want, st.totalBits, st.untakenBits)
			}
		}
	}
	if !found {
		t.Fatal("RealPacket not found in the pinned unit")
	}
}

// TestBoundsGateRefuses proves the §1.7 hard-bounds gate is live: a field
// count far too small must refuse, not emit.
func TestBoundsGateRefuses(t *testing.T) {
	_, _, err := generate(pinSeed, 40)
	if err == nil {
		t.Fatal("generate(seed, 40) emitted a schema; expected the [1000, 2000] bits refusal")
	}
	if !strings.Contains(err.Error(), "REFUSING") && !strings.Contains(err.Error(), "too small") {
		t.Fatalf("unexpected refusal message: %v", err)
	}
}

// The second pinned RealPacket instance (issue #246). The js reads round's
// review found the instance oracle's RealPacket coverage resting on ONE buffer:
// of the 24 pinned wire goldens exactly one (testdata/wire/real_packet.bin, the
// all-defaults instance) decodes as a RealPacket, so a single-leaf mutation walk
// only ever starts from one set of value bands. The general law it rides with is
// why a second INSTANCE — not a second buffer with the same values — is the fix:
// corpus bias is oracle blindness, and an oracle only tests the value bands the
// corpus feeds it.
//
// The second instance is the same all-defaults packet with its first ranged
// field (f001_int, wire [-805495, 805495]) driven to its MAXIMUM — the top of a
// band the all-defaults seed never reaches. Every other bit is the primary
// golden's, so the corpus gains a seed without moving any other field's band.
// realpacket-gen -alt-o re-pins both files; real_packet_alt.bits carries the
// §163 bit-exact count beside the bytes.
const (
	realPacketAltPath     = "../../../testdata/wire/real_packet_alt.bin"
	realPacketAltBitsPath = "../../../testdata/wire/real_packet_alt.bits"
	realPacketPath        = "../../../testdata/wire/real_packet.bin"
	realPacketBitsPath    = "../../../testdata/wire/real_packet.bits"
)

func TestSecondRealPacketInstancePinned(t *testing.T) {
	primary, err := os.ReadFile(realPacketPath)
	if err != nil {
		t.Fatalf("reading the primary RealPacket golden: %v", err)
	}
	alt, err := os.ReadFile(realPacketAltPath)
	if err != nil {
		t.Fatalf("the RealPacket oracle seed set is thin (schema #246): %v — pin the second instance", err)
	}
	if len(alt) != len(primary) {
		t.Fatalf("the second RealPacket instance is %d bytes, the first is %d — a fixed-width shape's instances ride one length (§2.7)",
			len(alt), len(primary))
	}
	if bytes.Equal(alt, primary) {
		t.Fatal("the second RealPacket instance is byte-identical to the first — a copy is not a second seed")
	}

	// the bit-exact measure oracle (#163): the reference writer's exact bit
	// count is pinned beside the bytes, and both instances ride the same width.
	primaryBits, err := pinnedBitCount(realPacketBitsPath)
	if err != nil {
		t.Fatal(err)
	}
	altBits, err := pinnedBitCount(realPacketAltBitsPath)
	if err != nil {
		t.Fatal(err)
	}
	if altBits != primaryBits {
		t.Fatalf("the second instance's pinned bit count is %d, the first's is %d — a fixed-width shape's instances ride one width",
			altBits, primaryBits)
	}

	// shape comes from the checked unit, never from a transcribed offset.
	st, err := realPacketStruct(pinPath)
	if err != nil {
		t.Fatal(err)
	}
	offset, field, err := firstRangedIntField(st)
	if err != nil {
		t.Fatal(err)
	}
	width := ir.BitsRequired(field.IntMin, field.IntMax)

	if got := decodeField(alt, offset, width, field.IntMin); got.Cmp(field.IntMax) != 0 {
		t.Fatalf("%s decodes to %s in the second instance, want its max %s", field.Name, got, field.IntMax)
	}
	if decodeField(primary, offset, width, field.IntMin).Cmp(field.IntMax) == 0 {
		t.Fatalf("%s already sits at max in the primary instance — the second instance adds no value band", field.Name)
	}
	if !equalOutsideField(primary, alt, offset, width) {
		t.Fatal("the second instance differs outside the pinned field — it is not the all-defaults packet with one leaf driven to max")
	}
}

func pinnedBitCount(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	var bits int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &bits); err != nil {
		return 0, fmt.Errorf("parsing %s: %w", path, err)
	}
	return bits, nil
}

func decodeField(data []byte, offset, width int64, min *big.Int) *big.Int {
	v := new(big.Int)
	for i := int64(0); i < width; i++ {
		at := offset + i
		if (data[at/8]>>uint(at%8))&1 == 1 {
			v.SetBit(v, int(i), 1)
		}
	}
	return v.Add(v, min)
}

func equalOutsideField(a, b []byte, offset, width int64) bool {
	if len(a) != len(b) {
		return false
	}
	mask := make([]byte, len(a))
	for i := int64(0); i < width; i++ {
		at := offset + i
		mask[at/8] |= 1 << uint(at%8)
	}
	for i := range a {
		if (a[i] &^ mask[i]) != (b[i] &^ mask[i]) {
			return false
		}
	}
	return true
}
