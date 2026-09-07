package java

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	g.pf("%sif (%s < 0 || %s > %d) throw new IllegalArgumentException(\"wstring length\");\n", ind, length, length, f.Type.Size)
	g.emitWriteOffset(length, big.NewInt(0), big.NewInt(f.Type.Size), ind)
	g.pf("%sfor (int wideIndex = 0; wideIndex < %s; wideIndex++) {\n", ind, length)
	g.pf("%s    v = %s[wideIndex];\n", ind, name)
	g.pf("%s    if (v == 0) throw new IllegalArgumentException(\"wstring null unit\");\n", ind)
	g.mergeW(32, ind+"    ")
	g.pf("%s}\n", ind)
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
