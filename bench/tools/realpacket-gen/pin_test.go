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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
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
