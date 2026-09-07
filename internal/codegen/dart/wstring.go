package dart

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	g.chunkFlush(ind)
	g.pf("%s{\n%s  final wideLength = %sLength;\n", ind, ind, name)
	g.pf("%s  if (wideLength < 0 || wideLength > %d) {\n%s    throw ArgumentError('wstring length');\n%s  }\n", ind, f.Type.Size, ind, ind)
	g.emitWriteOffset("wideLength", big.NewInt(0), big.NewInt(f.Type.Size), ind+"  ")
	g.chunkFlush(ind + "  ")
	g.emitByteReadLoop(ind+"  ", "wideIndex", "wideLength")
	g.pf("%s    v = %s[wideIndex];\n", ind, name)
	g.pf("%s    if (v == 0) {\n%s      throw ArgumentError('wstring null unit');\n%s    }\n", ind, ind, ind)
	g.mergeW(32, ind+"    ")
	g.pf("%s  }\n%s}\n", ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sif (bitsRead + %d > numBits) {\n%s  return false;\n%s}\n", ind, bits, ind, ind)
	g.readR(bits, ind)
	g.pf("%sif (v > %d) {\n%s  return false;\n%s}\n%s%s = v;\n", ind, f.Type.Size, ind, ind, ind, length)
	guard := fmt.Sprintf("%sif (bitsRead + %s * 32 > numBits) {", ind, length)
	if len(guard) <= 80 {
		g.pf("%s\n", guard)
	} else {
		g.pf("%sif (bitsRead + %s * 32 >\n%s    numBits) {\n", ind, length, ind)
	}
	g.pf("%s  return false;\n%s}\n", ind, ind)
	g.pf("%s{\n%s  var expectLow = false;\n", ind, ind)
	g.emitByteReadLoop(ind+"  ", "wideIndex", length)
	g.invalidateWindow()
	g.readR(32, ind+"    ")
	g.pf("%s    if (v == 0 || v > 0xFFFF) {\n%s      return false;\n%s    }\n", ind, ind, ind)
	g.pf("%s    final high = v >= 0xD800 && v <= 0xDBFF;\n", ind)
	g.pf("%s    final low = v >= 0xDC00 && v <= 0xDFFF;\n", ind)
	g.pf("%s    if (low != expectLow) {\n%s      return false;\n%s    }\n", ind, ind, ind)
	g.pf("%s    expectLow = high;\n%s    %s[wideIndex] = v;\n%s  }\n", ind, ind, name, ind)
	g.pf("%s  if (expectLow) {\n%s    return false;\n%s  }\n%s}\n", ind, ind, ind, ind)
	g.invalidateWindow()
}
