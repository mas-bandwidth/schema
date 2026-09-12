package java

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	// SPEC §4.12's two WRITE-side rules — the used length in [0, N], and no
	// zero code unit among the used units — are the CALLER's contract, and
	// write-side checks are DEBUG ONLY (SPEC §5). In Java that idiom is the
	// dormant `assert checkWrite<Name>(value, data)` predicate, so both rules
	// live in emitCheckScalar (functions.go) and nothing is emitted here.
	// They were `throw new IllegalArgumentException(...)`, alive in every
	// build. The READ side refuses both in every build.
	// The release path must not unwind either: the buffer holds N units, so an
	// out-of-contract length would throw ArrayIndexOutOfBoundsException. Clamp
	// into [0, N] once and write THAT length — deterministic bytes, never a
	// trap (SPEC §5). Math.clamp is JDK 21; the legs compile --release 17.
	g.pf("%s{\n", ind)
	g.pf("%s    final int wideUsed = Math.min(Math.max(%s, 0), %d);\n", ind, length, f.Type.Size)
	g.emitWriteOffset("wideUsed", big.NewInt(0), big.NewInt(f.Type.Size), ind+"    ")
	g.pf("%s    for (int wideIndex = 0; wideIndex < wideUsed; wideIndex++) {\n", ind)
	g.pf("%s        v = %s[wideIndex];\n", ind, name)
	g.mergeW(32, ind+"        ")
	g.pf("%s    }\n%s}\n", ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sif (bitsRead + %d > numBits) return false;\n", ind, bits)
	g.readR(bits, ind)
	g.pf("%sif (v > %d) return false;\n%s%s = (int) v;\n", ind, f.Type.Size, ind, length)
	g.pf("%sif (bitsRead + %s * 32L > numBits) return false;\n", ind, length)
	g.pf("%s{\n%s    boolean expectLow = false;\n", ind, ind)
	g.pf("%s    for (int wideIndex = 0; wideIndex < %s; wideIndex++) {\n", ind, length)
	g.readR(32, ind+"        ")
	g.pf("%s        if (v == 0 || v > 0xFFFF) return false;\n", ind)
	g.pf("%s        boolean high = v >= 0xD800 && v <= 0xDBFF;\n", ind)
	g.pf("%s        boolean low = v >= 0xDC00 && v <= 0xDFFF;\n", ind)
	g.pf("%s        if (low != expectLow) return false;\n", ind)
	g.pf("%s        expectLow = high;\n%s        %s[wideIndex] = (char) v;\n%s    }\n", ind, ind, name, ind)
	g.pf("%s    if (expectLow) return false;\n%s}\n", ind, ind)
}
