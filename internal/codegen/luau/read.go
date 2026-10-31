// Package luau: the type wire's READ side (docs/PORT-LUAU.md, THE CARD MAP).
// Every type, union and array reads through the vendored serialize.luau
// ReadStream (runtime/serialize*.luau): no private bit reader exists here, and
// a bug in the runtime is reported against the vendored function, never
// patched. The wire rules are docs/SPEC.md §4.3 (the bit stream) and §6.1
// (storage); the refusals are the reader's, returning false with nothing
// latched for a content verdict and the stream's own latched refusal reason
// (past_end, nonzero_padding, value_out_of_range: serialize format 2.0) for a
// stream refusal.
package luau

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("read", emitRead)
}

// emitRead appends the Read<Name> and Zero<Name> functions of every type and
// union in wire order (SPEC §6.1 items 2-4, §6.2).
func emitRead(g *Gen) error {
	g.Pf("%s", readHolders)
	g.Pf("%s", readUTF8)
	for _, d := range ir.EmissionOrder(g.File) {
		switch d := d.(type) {
		case *ir.Struct:
			emitReadStruct(g, d)
		case *ir.Union:
			emitReadUnion(g, d)
		}
	}
	return nil
}

// The module-local holders and the UTF-8 validator. A holder is always
// consumed in the same call that fills it (single threaded Lua, and generated
// code never holds one across a nested Read call). The validator is the JS
// leg's emitReadUTF8 translated: Unicode Table 3-7 ranges, no allocation.
const readHolders = `local BITS = { value = 0 }
local BIG = { value = 0i }
local BOOL = { value = false }
local STR = { value = "" }
local WIDE = { value = { lo = 0i, hi = 0i } }
`

const readUTF8 = `local function utf8valid(b: buffer, n: number): boolean
	local i = 0
	while i < n do
		local lead = buffer.readu8(b, i)
		i += 1
		if lead >= 0x80 then
			local rem, point
			if lead >= 0xC0 and lead <= 0xDF then rem, point = 1, bit32.band(lead, 0x1F)
			elseif lead >= 0xE0 and lead <= 0xEF then rem, point = 2, bit32.band(lead, 0x0F)
			elseif lead >= 0xF0 and lead <= 0xF7 then rem, point = 3, bit32.band(lead, 0x07)
			else return false end
			if rem > n - i then return false end
			for _ = 1, rem do
				local c = buffer.readu8(b, i)
				i += 1
				if c < 0x80 or c > 0xBF then return false end
				point = bit32.bor(bit32.lshift(point, 6), bit32.band(c, 0x3F))
			end
			if rem == 1 and point < 0x80 then return false end
			if rem == 2 and (point < 0x800 or (point >= 0xD800 and point <= 0xDFFF)) then return false end
			if rem == 3 and (point < 0x10000 or point > 0x10FFFF) then return false end
		end
	end
	return true
end
` // funcName maps a declaration reference to one of its generated functions.
func funcName(g *Gen, prefix, decl string) string {
	return g.Require(g.moduleOf(decl, false)) + "." + prefix + decl
}

// call emits the C++-style early-out around one serialize call.
func (g *Gen) call(ind, expr, comment string) {
	g.Pf("%sif not %s then%s\n%s  return false\n%send\n", ind, expr, comment, ind, ind)
}

func emitReadStruct(g *Gen, d *ir.Struct) {
	g.Pf("-- Init%s resets a value to what New%s builds, in place: its declared defaults\n", d.Name, d.Name)
	g.Pf("-- (SPEC §5), every buffer and nested value reused, so a reused value costs no\n")
	g.Pf("-- allocation.\n")
	g.Pf("function %s.Init%s(value: any)\n", g.Self, d.Name)
	if len(d.Fields) == 0 {
		g.Pf("  -- empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range d.Fields {
		emitInitField(g, f, "value."+ir.GoExportName(f.Name), "  ")
	}
	g.Pf("end\n\n")
	g.Pf("-- Zero%s resets the §5 ZERO form: all-zero storage, without declared defaults.\n", d.Name)
	g.Pf("function %s.Zero%s(value: any)\n", g.Self, d.Name)
	if len(d.Fields) == 0 {
		g.Pf("  -- empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range d.Fields {
		emitZeroField(g, f, "value", "  ")
	}
	g.Pf("end\n\n")

	g.Pf("-- Read%s reads the type wire (SPEC §4.3, §6.1); false refuses without a latch\n", d.Name)
	g.Pf("-- (a stream refusal rides stream:error(), named by the runtime's refusalName).\n")
	g.Pf("function %s.Read%s(stream: any, value: any): boolean\n", g.Self, d.Name)
	g.BulkBytes = ir.AlignedFixedByteArrays(d)
	defer func() { g.BulkBytes = nil }()
	if len(d.Items) == 0 {
		g.Pf("  -- empty body — presence is the payload (SPEC §4.6)\n")
	}
	emitReadItems(g, d.Items, "  ")
	g.Pf("  return true\nend\n\n")
}

func emitReadUnion(g *Gen, d *ir.Union) {
	g.Pf("-- Init%s resets the union to what New%s builds, in place: None, and every\n", d.Name, d.Name)
	g.Pf("-- payload arm at its declared defaults (SPEC §4.8).\n")
	g.Pf("function %s.Init%s(value: any)\n  value.Type = 0\n", g.Self, d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		f := *v.F
		f.Name = v.Name
		emitInitField(g, &f, "value."+ir.GoExportName(v.Name), "  ")
	}
	g.Pf("end\n\n")
	g.Pf("-- Zero%s resets the union to None; unselected arms are unspecified (SPEC §4.8).\n", d.Name)
	g.Pf("function %s.Zero%s(value: any)\n  value.Type = 0\nend\n\n", g.Self, d.Name)
	g.Pf("function %s.Read%s(stream: any, value: any): boolean\n", g.Self, d.Name)
	if d.Max == 0 {
		g.Pf("  value.Type = 0 -- zero wire bits — only None exists (SPEC §4.8)\n")
		g.Pf("  return true\nend\n\n")
		return
	}
	g.call("  ", fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", d.Max), " -- rejects a tag above the count (SPEC §4.8)")
	g.Pf("  value.Type = BITS.value\n")
	for _, v := range d.Variants {
		g.Pf("  if value.Type == %s then\n", tagValue(g, d, v))
		if v.Void() {
			g.Pf("    return true -- a payload-free arm: the tag is the whole wire (SPEC §4.8)\n")
			g.Pf("  end\n")
			continue
		}
		arm := ir.GoExportName(v.Name)
		g.Pf("    %s(value.%s) -- the selected arm starts at its declared defaults, in place (SPEC §4.8, §5)\n", funcName(g, "Init", v.Type), arm)
		g.Pf("    return %s(stream, value.%s)\n", funcName(g, "Read", v.Type), arm)
		g.Pf("  end\n")
	}
	g.Pf("  return true -- None\nend\n\n")
}

// tagValue is the tag enum member for a union variant, required from the
// union's own module or another file.
func tagValue(g *Gen, d *ir.Union, v ir.UnionVariant) string {
	return g.Self + "." + d.Name + "Type." + ir.GoExportName(v.Name)
}

func emitReadItems(g *Gen, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(g, item.F, ind)
		case *ir.ConstItem:
			emitConstItem(g, item, ind)
		case *ir.ReservedItem:
			emitReservedItem(g, item, ind)
		case *ir.AlignItem:
			g.call(ind, "stream:serializeAlign()", " -- rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "not "
			}
			g.Pf("%sif %svalue.%s then\n", ind, neg, ir.GoExportName(item.Cond))
			emitReadItems(g, item.Then, ind+"  ")
			emitZeroItems(g, item.Else, "value", ind+"  ")
			g.Pf("%selse\n", ind)
			if item.Else != nil {
				emitReadItems(g, item.Else, ind+"  ")
			}
			emitZeroItems(g, item.Then, "value", ind+"  ")
			g.Pf("%send\n", ind)
		}
	}
}

func emitConstItem(g *Gen, item *ir.ConstItem, ind string) {
	holder, fn, lit := "BITS", "serializeBits", item.Value.String()
	if item.Bits > 32 {
		holder, fn, lit = "BIG", "serializeBits64", IntegerLit(item.Value)
	}
	g.call(ind, fmt.Sprintf("stream:%s(%s, %d)", fn, holder, item.Bits), "")
	g.Pf("%sif %s.value ~= %s then -- const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, holder, lit, item.Value.String(), item.Bits)
	g.Pf("%s  return false\n%send\n", ind, ind)
}

func emitReservedItem(g *Gen, item *ir.ReservedItem, ind string) {
	holder, fn, zero := "BITS", "serializeBits", "0"
	if item.Bits > 32 {
		holder, fn, zero = "BIG", "serializeBits64", "0i"
	}
	g.call(ind, fmt.Sprintf("stream:%s(%s, %d)", fn, holder, item.Bits), "")
	g.Pf("%sif %s.value ~= %s then -- reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, holder, zero, item.Bits)
	g.Pf("%s  return false\n%send\n", ind, ind)
}

func emitReadField(g *Gen, f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := fmt.Sprintf("%d", f.ArrayBound)
		limit := bound
		if f.Array == ir.ArrayCounted {
			g.call(ind, fmt.Sprintf("stream:serializeInt(BITS, %d, %s)", f.ArrayMin, bound),
				" -- the count guards the loop (SPEC §6.3)")
			g.Pf("%s%sCount = BITS.value\n", ind, name)
			limit = name + "Count"
		}
		if isByteElem(f.Type) && !f.HasIntRange && f.Array == ir.ArrayFixed && g.BulkBytes[f] {
			// byte aligned by construction (ir.AlignedFixedByteArrays): the
			// bulk copy's align reads no bits, and the bytes land in storage
			g.Pf("%sif buffer.len(%s) ~= %s then return false end -- [N]uint8 storage is N bytes\n", ind, name, bound)
			g.call(ind, fmt.Sprintf("stream:serializeBytes(%s)", name), " -- [N]uint8 on a byte boundary: one bulk copy")
			return
		}
		g.Pf("%sfor i = 1, %s do\n", ind, limit)
		if isByteElem(f.Type) {
			g.Pf("%s  local item = 0\n", ind)
			emitReadScalar(g, f, "item", "item", ind+"  ")
			g.Pf("%s  buffer.writeu8(%s, i - 1, item)\n", ind, name)
		} else {
			emitReadScalar(g, f, name+"[i]", name+"Length[i]", ind+"  ")
		}
		g.Pf("%send\n", ind)
		return
	}
	emitReadScalar(g, f, name, name+"Length", ind)
}

func emitReadScalar(g *Gen, f *ir.Field, name, lenName, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			g.Pf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, rawLiteral(f.Type, rawMin))
			return
		}
		if f.Type.Width > 64 {
			g.call(ind, fmt.Sprintf("stream:serializeFixed128(WIDE, %d, %d, %s, %s)",
				f.Type.IntBits, f.Type.FracBits, IntegerLit(f.IntMin), IntegerLit(f.IntMax)), "")
			g.Pf("%s%s = WIDE.value\n", ind, name)
			return
		}
		g.call(ind, fmt.Sprintf("stream:serializeFixed64(BIG, %d, %d, %s, %s)",
			f.Type.IntBits, f.Type.FracBits, IntegerLit(f.IntMin), IntegerLit(f.IntMax)), "")
		if f.Type.Width > 32 {
			g.Pf("%s%s = BIG.value\n", ind, name)
		} else {
			g.Pf("%s%s = integer.tonumber(BIG.value)\n", ind, name)
		}
	case ir.TInt:
		if f.Type.Width == 128 {
			holder := "WIDE"
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					g.Pf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, Int128Lit(f.IntMin))
					return
				}
				g.call(ind, fmt.Sprintf("stream:serializeInt128(%s, %s, %s)", holder,
					Int128Lit(f.IntMin), Int128Lit(f.IntMax)), "")
			} else {
				g.call(ind, fmt.Sprintf("stream:serializeUint128(%s)", holder), "")
			}
			g.Pf("%s%s = %s.value\n", ind, name, holder)
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				g.Pf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, rawLiteral(f.Type, f.IntMin))
				return
			}
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				g.call(ind, fmt.Sprintf("stream:serializeInt(BITS, %s, %s)", f.IntMin.String(), f.IntMax.String()), "")
				g.Pf("%s%s = BITS.value\n", ind, name)
			case "int64":
				g.call(ind, fmt.Sprintf("stream:serializeInt64(BIG, %s, %s)", IntegerLit(f.IntMin), IntegerLit(f.IntMax)), "")
				if f.Type.Width <= 32 {
					g.Pf("%s%s = integer.tonumber(BIG.value)\n", ind, name)
				} else {
					g.Pf("%s%s = BIG.value\n", ind, name)
				}
			default:
				bits := ir.BitsRequired(f.IntMin, f.IntMax)
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", bits), "")
				if diff.Cmp(maxUint64) != 0 {
					if diff.Cmp(maxInt64) > 0 {
						g.Pf("%sif integer.ult(%s, BIG.value) then -- a read rejects out-of-range (SPEC §5)\n", ind, IntegerLit(diff))
					} else {
						g.Pf("%sif integer.gt(BIG.value, %s) then -- a read rejects out-of-range (SPEC §5)\n", ind, IntegerLit(diff))
					}
					g.Pf("%s  return false\n%send\n", ind, ind)
				}
				switch {
				case f.IntMin.Sign() == 0 && f.Type.Width <= 32:
					g.Pf("%s%s = integer.tonumber(BIG.value)\n", ind, name)
				case f.IntMin.Sign() == 0:
					g.Pf("%s%s = BIG.value\n", ind, name)
				case f.Type.Width <= 32:
					g.Pf("%s%s = integer.tonumber(integer.add(BIG.value, %s))\n", ind, name, IntegerLit(f.IntMin))
				default:
					g.Pf("%s%s = integer.add(BIG.value, %s)\n", ind, name, IntegerLit(f.IntMin))
				}
			}
			return
		}
		emitReadBareInt(g, f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
			g.Pf("%s%s = BITS.value\n", ind, name)
		} else {
			g.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", f.Type.Width), "")
			g.Pf("%s%s = BIG.value\n", ind, name)
		}
	case ir.TBool:
		g.call(ind, "stream:serializeBool(BOOL)", "")
		g.Pf("%s%s = BOOL.value\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.call(ind, fmt.Sprintf("stream:serializeCompressedFloat(STR, %s, %s, %s)",
				FormatFloat(f.FMin), FormatFloat(f.FMax), FormatFloat(f.Resolution)), "")
			g.Pf("%s%s = STR.value\n", ind, name)
			return
		}
		g.call(ind, "stream:serializeFloat(STR)", "")
		g.Pf("%s%s = STR.value\n", ind, name)
	case ir.TFloat64:
		g.call(ind, "stream:serializeDouble(STR)", "")
		g.Pf("%s%s = STR.value\n", ind, name)
	case ir.TWString:
		emitReadWString(g, f, name, lenName, ind)
	case ir.TString, ir.TBytes:
		g.Pf("%sdo -- length, align, bytes into the pre-allocated buffer (SPEC §4.7)\n", ind)
		g.call(ind+"  ", fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", f.Type.Size),
			" -- the length guards the payload (§6.3)")
		g.Pf("%s  local n = BITS.value\n", ind)
		g.Pf("%s  local tmp = buffer.create(n)\n", ind)
		g.call(ind+"  ", "stream:serializeBytes(tmp)", "")
		g.Pf("%s  buffer.copy(%s, 0, tmp, 0, n) -- nothing past the used length is written (§5's tail rule)\n", ind, name)
		g.Pf("%s  %s = n\n", ind, lenName)
		if f.Type.Kind == ir.TString {
			g.Pf("%s  if not utf8valid(%s, n) then return false end -- malformed UTF-8 (SPEC §4.7)\n", ind, name)
			g.Pf("%s  for k = 0, n - 1 do if buffer.readu8(%s, k) == 0 then return false end end -- interior null (SPEC §4.7)\n", ind, name)
		}
		g.Pf("%send\n", ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.call(ind, fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", ref.Max), "")
			g.Pf("%s%s = BITS.value\n", ind, name)
		case *ir.Flags:
			if ref.WireBits <= 32 {
				g.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", ref.WireBits), "")
			} else {
				g.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", ref.WireBits), "")
				g.Pf("%s%s = BIG.value\n", ind, name)
				return
			}
			g.Pf("%s%s = integer.create(BITS.value)\n", ind, name)
		case *ir.Struct, *ir.Union:
			g.call(ind, fmt.Sprintf("%s(stream, %s)", funcName(g, "Read", f.Type.Name), name), "")
		}
	}
}

func emitReadBareInt(g *Gen, f *ir.Field, name, ind string) {
	switch {
	case f.Type.Width == 64:
		g.call(ind, "stream:serializeBits64(BIG, 64)", "")
		g.Pf("%s%s = BIG.value\n", ind, name)
	case !f.Type.Signed && f.Type.Width == 32:
		g.call(ind, "stream:serializeBits(BITS, 32)", "")
		g.Pf("%s%s = BITS.value\n", ind, name)
	case f.Type.Signed:
		g.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
		// Lua's >> is logical; recover the sign through the same-width
		// unsigned round trip (SPEC §4.3).
		// The scratch local lives in its own do-block, so two signed fields in
		// one reader never redeclare it (luau-analyze LocalShadow).
		g.Pf("%sdo\n", ind)
		g.Pf("%s  local raw = BITS.value\n", ind)
		g.Pf("%s  %s = if raw >= %s then raw - %s else raw\n", ind, name,
			new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width-1)).String(),
			new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)).String())
		g.Pf("%send\n", ind)
	default:
		g.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
		g.Pf("%s%s = BITS.value\n", ind, name)
	}
}

// emitReadWString reads a wstring(N) field (SPEC §4.12), composed from
// primitives because serialize format 2.0 carries no wide-string call: the
// length in [0, N] over bits_required(0, N) bits, then each code unit as a
// 32-bit group, with no alignment anywhere. A group above 0xFFFF, a zero
// group, and an unpaired surrogate (a high one not followed by a low one, a
// low one not preceded by a high one, a high one last) refuse the read. The
// code units land in storage as they are; nothing past the used length is
// written (§5's tail rule).
func emitReadWString(g *Gen, f *ir.Field, name, lenName, ind string) {
	g.Pf("%sdo -- wstring(%d): the length, then 32-bit groups of UTF-16 code units (SPEC §4.12)\n", ind, f.Type.Size)
	g.call(ind+"  ", fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", f.Type.Size), " -- the length guards the groups (§6.3)")
	g.Pf("%s  local n = BITS.value\n", ind)
	g.Pf("%s  local high = false\n", ind)
	g.Pf("%s  for k = 0, n - 1 do\n", ind)
	g.call(ind+"    ", "stream:serializeBits(BITS, 32)", "")
	g.Pf("%s    local unit = BITS.value\n", ind)
	g.Pf("%s    if unit == 0 or unit > 0xFFFF then return false end -- a zero group, or not a code unit\n", ind)
	g.Pf("%s    if unit >= 0xD800 and unit <= 0xDBFF then\n", ind)
	g.Pf("%s      if high then return false end -- a high surrogate not followed by a low one\n", ind)
	g.Pf("%s      high = true\n", ind)
	g.Pf("%s    elseif unit >= 0xDC00 and unit <= 0xDFFF then\n", ind)
	g.Pf("%s      if not high then return false end -- a low surrogate not preceded by a high one\n", ind)
	g.Pf("%s      high = false\n", ind)
	g.Pf("%s    elseif high then\n", ind)
	g.Pf("%s      return false -- a high surrogate not followed by a low one\n", ind)
	g.Pf("%s    end\n", ind)
	g.Pf("%s    buffer.writeu16(%s, k * 2, unit)\n", ind, name)
	g.Pf("%s  end\n", ind)
	g.Pf("%s  if high then return false end -- a high surrogate last\n", ind)
	g.Pf("%s  %s = n\n", ind, lenName)
	g.Pf("%send\n", ind)
}

// rawLiteral renders a value in its field's storage domain (docs/PORT-LUAU.md,
// THE TYPE MAPPING): a number at 32 bits or fewer, an integer at 64, an
// Int128 pair at 128.
func rawLiteral(t ir.FieldType, v *big.Int) string {
	switch {
	case t.Width > 64:
		return Int128Lit(v)
	case t.Width > 32:
		return IntegerLit(v)
	}
	return v.String()
}

func emitZeroItems(g *Gen, items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitZeroField(g, item.F, path, ind)
		case *ir.Branch:
			emitZeroItems(g, item.Then, path, ind)
			emitZeroItems(g, item.Else, path, ind)
		}
	}
}

func emitZeroField(g *Gen, f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		g.Pf("%sbuffer.fill(%s, 0, 0)\n%s%sLength = 0\n", ind, name, ind, name)
	case f.Array != ir.ArrayNone:
		switch f.Type.Kind {
		case ir.TString, ir.TBytes, ir.TWString:
			g.Pf("%sfor i = 1, %d do buffer.fill(%s[i], 0, 0); %sLength[i] = 0 end\n", ind, f.ArrayBound, name, name)
		case ir.TNamed:
			switch f.Type.Ref.(type) {
			case *ir.Struct, *ir.Union:
				g.Pf("%sfor i = 1, %d do %s(%s[i]) end\n", ind, f.ArrayBound,
					funcName(g, "Zero", f.Type.Name), name)
			default:
				g.Pf("%sfor i = 1, %d do %s[i] = %s end\n", ind, f.ArrayBound, name, ZeroValue(g, f.Type))
			}
		default:
			g.Pf("%sfor i = 1, %d do %s[i] = %s end\n", ind, f.ArrayBound, name, ZeroValue(g, f.Type))
		}
		if f.Array == ir.ArrayCounted {
			g.Pf("%s%sCount = 0\n", ind, name)
		}
	case f.Type.Kind == ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			g.Pf("%s%s(%s)\n", ind, funcName(g, "Zero", f.Type.Name), name)
		case *ir.Union:
			g.Pf("%s%s(%s)\n", ind, funcName(g, "Zero", f.Type.Name), name)
		default:
			g.Pf("%s%s = %s\n", ind, name, ZeroValue(g, f.Type))
		}
	default:
		g.Pf("%s%s = %s\n", ind, name, ZeroValue(g, f.Type))
	}
}

// intRangePath picks the serialize.luau call family for a ranged integer —
// the same switch as the other targets, so every emitter reads identical
// wire (SPEC §4.3, §6.3).
func intRangePath(min, max *big.Int) string {
	i32 := big.NewInt(1<<31 - 1)
	i32lo := big.NewInt(-1 << 31)
	if min.Cmp(i32lo) >= 0 && max.Cmp(i32) <= 0 {
		return "int32"
	}
	i64 := new(big.Int).SetInt64(1<<63 - 1)
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	if min.Cmp(i64lo) >= 0 && max.Cmp(i64) <= 0 {
		return "int64"
	}
	return "bits64"
}

var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

func ZeroValue(g *Gen, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return g.ctor(t.Name, false)
		case *ir.Flags:
			return "0i" // flags store uint64 as an integer (docs/PORT-LUAU.md)
		}
		return "0"
	}
	if t.Width > 64 {
		return "_Rt.int128(0i, 0i)"
	}
	if t.Width > 32 {
		return "0i"
	}
	return "0"
}

// emitInitField resets one field to its construction value (what New<Name>
// builds, SPEC §5) without replacing any table or buffer the value already
// holds: a buffer is zeroed and takes its default bytes again, a nested value
// and every array element is reset in place, a counted array returns to its
// born count. A 128-bit value is a fresh pair (an Int128 is a value).
func emitInitField(g *Gen, f *ir.Field, name, ind string) {
	t := f.Type
	switch {
	case f.Array != ir.ArrayNone:
		switch {
		case t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString:
			g.Pf("%sfor i = 1, %d do buffer.fill(%s[i], 0, 0); %sLength[i] = 0 end\n", ind, f.ArrayBound, name, name)
		case isByteElem(t):
			g.Pf("%sbuffer.fill(%s, 0, 0)\n", ind, name)
		case isInitable(g, t):
			g.Pf("%sfor i = 1, %d do %s(%s[i]) end\n", ind, f.ArrayBound, funcName(g, "Init", t.Name), name)
		default:
			_, init := g.elem(f)
			g.Pf("%sfor i = 1, %d do %s[i] = %s end\n", ind, f.ArrayBound, name, init)
		}
		if f.Array == ir.ArrayCounted {
			g.Pf("%s%sCount = %d -- the born count (SPEC §4.6)\n", ind, name, f.BornCount())
		}
	case t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString:
		g.Pf("%sbuffer.fill(%s, 0, 0)\n%s%sLength = 0\n", ind, name, ind, name)
		for _, line := range g.ByteDefault(f, name) {
			g.Pf("%s%s\n", ind, line)
		}
	case isInitable(g, t):
		g.Pf("%s%s(%s)\n", ind, funcName(g, "Init", t.Name), name)
	default:
		_, init := g.scalar(f)
		g.Pf("%s%s = %s\n", ind, name, init)
	}
}

// isInitable reports a named packet type or union, which has an Init<Name>.
func isInitable(g *Gen, t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch r := t.Ref.(type) {
	case *ir.Struct:
		return !r.IsTable
	case *ir.Union:
		return g.Unit.TableUnions[t.Name] == nil
	}
	return false
}
