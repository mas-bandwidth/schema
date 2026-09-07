package js

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func emitReadWStringBody(pf func(string, ...any), name, length, ind string, readGroup func(string)) {
	pf("%s{\n%s  let expectLow = false;\n", ind, ind)
	pf("%s  for (let wideIndex = 0; wideIndex < %s; wideIndex++) {\n", ind, length)
	readGroup(ind + "    ")
	pf("%s    if (wideGroup === 0 || wideGroup > 0xFFFF) return false;\n", ind)
	pf("%s    const high = wideGroup >= 0xD800 && wideGroup <= 0xDBFF;\n", ind)
	pf("%s    const low = wideGroup >= 0xDC00 && wideGroup <= 0xDFFF;\n", ind)
	pf("%s    if (low !== expectLow) return false;\n", ind)
	pf("%s    expectLow = high;\n%s    %s[wideIndex] = wideGroup;\n%s  }\n", ind, ind, name, ind)
	pf("%s  if (expectLow) return false;\n%s}\n", ind, ind)
}

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	length, scratch := name+"Length", g.numScratch()
	g.emitWriteFoldedNum(length, "0", g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size)), big.NewInt(0), big.NewInt(f.Type.Size), true, "", ind)
	g.pf("%sfor (let wideIndex = 0; wideIndex < %s; wideIndex++) {\n", ind, length)
	g.pf("%s  %s.value = %s[wideIndex];\n", ind, scratch, name)
	g.pf("%s  if (%s.value === 0) return false;\n", ind, scratch)
	g.call(ind+"  ", fmt.Sprintf("stream.serializeBits(%s, 32)", scratch), "")
	g.pf("%s}\n", ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	length, scratch := name+"Length", g.numScratch()
	g.call(ind, fmt.Sprintf("stream.serializeInt(%s, 0, %s)", scratch, g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size))), "")
	g.pf("%s%s = %s.value;\n", ind, length, scratch)
	emitReadWStringBody(g.pf, name, length, ind, func(i string) {
		g.call(i, fmt.Sprintf("stream.serializeBits(%s, 32)", scratch), "")
		g.pf("%sconst wideGroup = %s.value;\n", i, scratch)
	})
}

func (g *fgen) emitWriteWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	g.pf("%sif (!Number.isInteger(%s) || %s < 0 || %s > %d) return -1;\n", ind, length, length, length, f.Type.Size)
	g.emitWriteRangedNum(length, 0, f.Type.Size, ind)
	g.chunkFlush(ind)
	g.pf("%sfor (let wideIndex = 0; wideIndex < %s; wideIndex++) {\n", ind, length)
	g.pf("%s  v = %s[wideIndex];\n%s  if (v === 0) return -1;\n", ind, name, ind)
	g.mergeW(32, ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fgen) emitReadWString(f *ir.Field, name, ind string) {
	length := name + "Length"
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.pf("%sif (br + %d > numBits) return false;\n", ind, bits)
	g.readR(bits, ind)
	g.pf("%sif (v > %d) return false;\n%s%s = v;\n", ind, f.Type.Size, ind, length)
	g.pf("%sif (br + %s * 32 > numBits) return false;\n", ind, length)
	emitReadWStringBody(g.pf, name, length, ind, func(i string) {
		g.readR(32, i)
		g.pf("%sconst wideGroup = v;\n", i)
	})
}
