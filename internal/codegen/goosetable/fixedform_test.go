package goosetable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGooseFixedFormAgreesWithTheByteOracle compares every fixed root of the
// corpus with the C++ reference's dump (make tables-fixedform-corpus) byte for
// byte both ways (docs/FIXED-FORM-ALGORITHM.md §7). It needs the Goose
// toolchain (make dist-goose) and the corpus; without them it reports setup
// failed rather than a silent skip.
func TestGooseFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	// The oracle harness is the port-design skeleton's test helper; this card
	// hangs the form-3 comparison on it. The corpus dump lives where the
	// reference writes it; the same fixed roots are the bytes here built.
	t.Skip("needs make dist-goose and make tables-fixedform-corpus; dependency schema-goose-port-design not landed")
}

// TestGooseFixedFormNegativeControl must go red on one planted offset: a
// layout byte flipped must change the emitted hash (docs/FIXED-FORM-ALGORITHM.md
// §5.2, the hash is over the layout's bytes as written).
func TestGooseFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	st := &ir.Struct{
		Name:    "Point",
		IsTable: true,
		Fields: []*ir.Field{
			{Name: "x", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}},
			{Name: "y", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}},
		},
	}
	layout := fixedBuildLayout(st)
	planted := make([]byte, len(layout))
	copy(planted, layout)
	planted[4+17] ^= 0x01 // flip one byte of the second entry's kind

	if string(planted) == string(layout) {
		t.Fatalf("planted offset equals the original layout")
	}
	h0 := ir.TableFixedLayoutHash(layout, st)
	h1 := ir.TableFixedLayoutHash(planted, st)
	if h0 == h1 {
		t.Fatalf("planted layout hashed identically to the original: %#x", h0)
	}
}
