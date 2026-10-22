package cstable

// fixedroadmap_t06_r30 — cs/R30 asserted WHOLE on the C# leg
// (docs/roadmap-evidence/fixed-cs.sexp): a compressed float rides as the float
// (docs/SPEC-TABLES.md §3.4), so min/max/resolution are DEFINITIONS in the
// digest, the step is the digest's 'Q' row, FINER WIDENS (two byte-identical
// layouts whose step differs move the digest, which is what lets a COARSER step
// refuse by the hash), and FP-CONTRACT IS OFF on this leg, so the emitted module
// and runtime carry no fused multiply-add. "coarser refuses by name" and
// "dropping the triple widens" are §5.1's lock WIDENS rules, held on this leg
// by internal/lockfile/cfloatrange_test.go (TestLockCFloatResolutionRefinedAllowed,
// TestLockCFloatResolutionCoarsenedRefuses, TestLockCFloatRangeDroppedAllowed);
// this file carries the direct C# emitter assertions the weak row lacked.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCsCompressedFloatLaw asserts cs/R30 whole on the C# leg. The three bounds
// of the law are proven here: (1) the emitted write and clamp bodies carry
// neither the resolution constant nor a rounding call — the field rides as the
// float32 bits; (2) two units differing only in resolution (0.1 and 0.01)
// produce different TableFixedDefinitionsDigest values and both carry the 'Q'
// step, so a finer step moves the hash and a coarser reader is refused by the
// hash; (3) the emitted C# fixed module and runtime carry no fused multiply-add
// (Math.FusedMultiplyAdd, MathF.FusedMultiplyAdd) and no Vector fused form,
// which is what "fp-contract off" means for a C# emitter. .NET gives no
// compiler contraction switch, so absence of the call is the whole claim.
func TestCsCompressedFloatLaw(t *testing.T) {
	t.Parallel()

	src := generateCS(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)

	write := csFn(src, "public static void TFixedWriteBody(")
	if write == "" {
		t.Fatal("R30: TFixedWriteBody is not emitted")
	}
	clamp := csFn(src, "public static void TFixedClampBody(")
	if clamp == "" {
		t.Fatal("R30: TFixedClampBody is not emitted")
	}

	// (1) RIDES AS THE FLOAT: the write and clamp bodies carry neither the
	// resolution constant nor a rounding call — the resolution is a definition
	// and never arithmetic on the value, so the step is in no emitted body.
	t06Has(t, write, "BinaryPrimitives.WriteSingleLittleEndian",
		"R30 the compressed field rides as the float32 bits, never a quantized index")
	for _, body := range []struct{ name, s string }{
		{"write", write}, {"clamp", clamp},
	} {
		if strings.Contains(body.s, "0.01") {
			t.Errorf("R30: the resolution is a definition and not arithmetic, but the emitted %s body carries it:\n%s", body.name, body.s)
		}
		if strings.Contains(body.s, ".Round(") || strings.Contains(body.s, "resolution") {
			t.Errorf("R30: the %s body quantizes a compressed float; on this form it rides as the float:\n%s", body.name, body.s)
		}
	}

	// (2) FINER WIDENS, COARSER REFUSES BY THE HASH: two units differing only in
	// resolution move the definitions digest, and both carry the 'Q' step.
	coarse := unitFrom(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.1
}
`)
	fine := unitFrom(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	dCoarse := ir.TableFixedDefinitionsDigest(ir.TableFixedRoots(coarse)[0])
	dFine := ir.TableFixedDefinitionsDigest(ir.TableFixedRoots(fine)[0])
	if !bytes.Contains(dCoarse, []byte{'Q'}) || !bytes.Contains(dFine, []byte{'Q'}) {
		t.Error("R30: the resolution is not in the definitions digest under 'Q' (§5.2)")
	}
	if bytes.Equal(dCoarse, dFine) {
		t.Error("R30: a finer step must move the digest (a coarser step then refuses by the hash), but 0.1 and 0.01 hash the same")
	}

	// (3) FP-CONTRACT OFF: .NET gives no compiler contraction switch, so the
	// claim is exactly that no fused multiply-add call is emitted — not in the
	// fixed module (the generated output) nor in the shared runtime.
	for _, banned := range []string{"Math.FusedMultiplyAdd", "MathF.FusedMultiplyAdd", "Vector"} {
		for _, what := range []struct{ name, s string }{
			{"fixed module", src}, {"runtime types", tableFixedRuntimeTypes}, {"runtime wire", tableFixedWireSource},
		} {
			if strings.Contains(what.s, banned) {
				t.Errorf("R30: fp-contract is off on every leg, but the emitted C# %s carries %q", what.name, banned)
			}
		}
	}
}
