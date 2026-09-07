package elixir

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	g.needWString = true
	g.raiseIf(fmt.Sprintf("rem(byte_size(%s), 2) != 0", name), "wstring needs complete 16-bit code units", ind)
	g.raiseIf(fmt.Sprintf("byte_size(%s) > %d", name, f.Type.Size*2), "wstring length exceeds its code-unit bound", ind)
	g.pf("%sv = byte_size(%s) >>> 1\n", ind, name)
	g.mergeW(ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), ind)
	g.flushW(ind)
	g.syncSB(ind)
	g.ensureScratch(ind, true)
	g.callAssign("{data, scratch, scratch_bits}", fmt.Sprintf("write_wstring(%s, data, scratch, scratch_bits)", name), ind)
	g.sbKnown, g.scZero = false, false
	g.scBound, g.sbBound = true, true
}

func (g *gen) emitReadWString(f *ir.Field, lv, ind string) {
	g.needWString, g.needRd = true, true
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	g.throwIf(fmt.Sprintf("bits_read + %d > num_bits", bits), "", ind)
	g.readR(bits, ind)
	g.throwIf(fmt.Sprintf("v > %d", f.Type.Size), "the length guards the wide groups", ind)
	g.pf("%slen = v\n", ind)
	g.throwIf("bits_read + len * 32 > num_bits", "", ind)
	g.rdBreak()
	g.callAssign("{bits_read, "+lv+"}", "read_wstring(len, data, bits_read, false, <<>>)", ind)
}

const wstringHelpers = `  defp write_wstring(<<>>, data, scratch, scratch_bits), do: {data, scratch, scratch_bits}

  defp write_wstring(<<unit::little-16, rest::binary>>, data, scratch, scratch_bits) do
    if unit == 0 do
      raise ArgumentError, "wstring null unit"
    end

    group = scratch ||| unit <<< scratch_bits
    data = <<data::binary, group::little-32>>
    write_wstring(rest, data, group >>> 32, scratch_bits)
  end

  defp read_wstring(0, _data, _bits_read, true, _acc), do: throw(:invalid)
  defp read_wstring(0, _data, bits_read, false, acc), do: {bits_read, acc}

  defp read_wstring(remaining, data, bits_read, expect_low, acc) do
    unit = rd(data, bits_read, 32)

    if unit == 0 or unit > 0xFFFF do
      throw(:invalid)
    end

    high = unit >= 0xD800 and unit <= 0xDBFF
    low = unit >= 0xDC00 and unit <= 0xDFFF

    if low != expect_low do
      throw(:invalid)
    end

    read_wstring(remaining - 1, data, bits_read + 32, high, <<acc::binary, unit::little-16>>)
  end

`
