package csharp

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	length := "value." + g.m(g.fieldBase(f)+"Length")
	bound := g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false)
	// SPEC §4.12's write-side rules — the used length in [0, N], and no zero
	// code unit among the used units — are the WRITER's contract, "in that
	// target's own idiom (§5)": in C# that idiom is Debug.Assert, gone from a
	// release build. The read side refuses both in every build.
	g.writeAssert(ind, fmt.Sprintf("%s >= 0 && %s <= %s", length, length, bound),
		fmt.Sprintf("%s out of range [0, %s]", length, bound), "")
	// And the RELEASE path must not unwind either: the buffer holds N units, so
	// an out-of-contract length indexes a char[N] out of bounds and throws
	// IndexOutOfRangeException — the one thing SPEC §5 says no target does
	// outside Elixir. Clamp into [0, N] once and write THAT length: the wire
	// carries the clamped length and the clamped payload, so a release write of
	// a bad length gives the bytes of the clamped write, byte for byte, and
	// never a trap.
	g.needsSystem = true
	g.sf("%s{\n", ind)
	g.sf("%s    int clampedLength = Math.Clamp(%s, 0, %s); // release: an out-of-contract length writes the clamped length — never a trap (§5)\n",
		ind, length, bound)
	g.emitWriteFoldedRange("clampedLength", "0", bound, big.NewInt(0), big.NewInt(f.Type.Size), false, false, false, "", ind+"    ")
	g.sf("%s    for (int wideIndex = 0; wideIndex < clampedLength; wideIndex++)\n%s    {\n", ind, ind)
	g.sf("%s        uint wideGroup = %s[wideIndex];\n", ind, name)
	g.writeAssert(ind+"        ", "wideGroup != 0", "a wide string's used units carry an interior null", "")
	g.call(ind+"        ", fmt.Sprintf("%s.SerializeBits(ref wideGroup, 32)", g.rv()), "")
	g.sf("%s    }\n%s}\n", ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	length := "value." + g.m(g.fieldBase(f)+"Length")
	g.call(ind, fmt.Sprintf("%s.SerializeInt(ref %s, 0, %s)", g.rv(), length, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "int", false)), "")
	g.sf("%s{\n%s    bool expectLow = false;\n", ind, ind)
	g.sf("%s    for (int wideIndex = 0; wideIndex < %s; wideIndex++)\n%s    {\n", ind, length, ind)
	g.sf("%s        uint wideGroup = 0;\n", ind)
	g.call(ind+"        ", fmt.Sprintf("%s.SerializeBits(ref wideGroup, 32)", g.rv()), "")
	g.sf("%s        if (wideGroup == 0 || wideGroup > 0xFFFF) return false;\n", ind)
	g.sf("%s        bool high = wideGroup >= 0xD800 && wideGroup <= 0xDBFF;\n", ind)
	g.sf("%s        bool low = wideGroup >= 0xDC00 && wideGroup <= 0xDFFF;\n", ind)
	g.sf("%s        if (low != expectLow) return false;\n", ind)
	g.sf("%s        expectLow = high;\n%s        %s[wideIndex] = (char)wideGroup;\n%s    }\n", ind, ind, name, ind)
	g.sf("%s    if (expectLow) return false;\n%s}\n", ind, ind)
}
