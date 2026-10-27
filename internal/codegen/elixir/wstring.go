package elixir

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	g.use("write_wstring/4")
	g.raiseIf(fmt.Sprintf("rem(byte_size(%s), 2) != 0", name), "wstring needs complete 16-bit code units", ind)
	g.raiseIf(fmt.Sprintf("byte_size(%s) > %d", name, f.Type.Size*2), "wstring length exceeds its code-unit bound", ind)
	g.pf("%sv = byte_size(%s) >>> 1\n", ind, name)
	g.mergeW(ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), ind)
	g.flushW(ind)
	g.syncSB(ind)
	g.ensureScratch(ind, true)
	g.callAssign("{data, scratch, scratch_bits}", fmt.Sprintf("write_wstring(%s, %s, scratch, scratch_bits)", name, g.dataArg()), ind)
	g.dataBound = true
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true
}

func (g *gen) emitReadWString(f *ir.Field, lv, ind string) {
	g.use("read_wstring/5")
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.throwIf(fmt.Sprintf("%s > num_bits", g.rpos(bits)), "", ind)
	g.readR(bits, ind)
	g.throwIf(fmt.Sprintf("v > %d", f.Type.Size), "the length guards the wide groups", ind)
	g.pf("%slen = v\n", ind)
	g.throwIf(fmt.Sprintf("%s + len * 32 > num_bits", g.rpos(0)), "", ind)
	g.rdBreak()
	g.callAssign("{bits_read, "+lv+"}", fmt.Sprintf("read_wstring(len, data, %s, false, <<>>)", g.rpos(0)), ind)
	g.rdDynamic(false)
}
