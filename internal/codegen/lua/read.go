// Package lua: the type wire's READ side (docs/PORT-LUA.md, THE CARD MAP).
// Every type, union and array reads through the vendored serialize.lua
// ReadStream (runtime/serialize*.lua): no private bit reader exists here, and
// a bug in the runtime is reported against the vendored function, never
// patched. The wire rules are docs/SPEC.md §4.3 (the bit stream) and §6.1
// (storage); the refusals are the reader's, returning false with nothing
// latched for a content verdict and the stream's own latch for an overflow.
package lua

import (
	"embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

//go:embed runtime/*.lua
var readRuntime embed.FS

func init() {
	RegisterPass(emitRead)
	RegisterRuntime(readRuntime)
}

// emitRead appends the Read<Name> and Zero<Name> functions of every type and
// union in wire order (SPEC §6.1 items 2-4, §6.2).
func emitRead(m *Module) error {
	for _, d := range ir.EmissionOrder(m.File) {
		switch d := d.(type) {
		case *ir.Struct:
			emitReadStruct(m, d)
		case *ir.Union:
			emitReadUnion(m, d)
		}
	}
	// Emit only prelude symbols referenced by this module's readers. A unit can
	// have no wire declarations, or a reader that uses only one holder.
	body := m.Body.String()
	for _, holder := range []struct{ name, declaration string }{
		{"BITS", "local BITS = { value = 0 }\n"},
		{"BIG", "local BIG = { value = 0 }\n"},
		{"BOOL", "local BOOL = { value = false }\n"},
		{"BYTES", "local BYTES = { value = \"\" }\n"},
		{"WIDE", "local WIDE = { value = { lo = 0, hi = 0 } }\n"},
	} {
		if strings.Contains(body, holder.name+".value") {
			m.Helper("read_holder_"+holder.name, holder.declaration)
		}
	}
	if strings.Contains(body, "utf8valid(") {
		m.Helper("read_utf8", readUTF8)
	}
	return nil
}

// A holder is consumed in the same call that fills it (single threaded Lua,
// and generated code never holds one across a nested Read call). The validator
// is the JS leg's emitReadUTF8 translated: Unicode Table 3-7 ranges.
const readUTF8 = `local function utf8valid(s)
  local i, n = 1, #s
  while i <= n do
    local b = string.byte(s, i)
    i = i + 1
    if b >= 0x80 then
      local rem, point
      if b >= 0xC0 and b <= 0xDF then
        rem, point = 1, b & 0x1F
      elseif b >= 0xE0 and b <= 0xEF then
        rem, point = 2, b & 0x0F
      elseif b >= 0xF0 and b <= 0xF7 then
        rem, point = 3, b & 0x07
      else
        return false
      end
      if rem > n - i + 1 then return false end
      for _ = 1, rem do
        local c = string.byte(s, i)
        i = i + 1
        if c < 0x80 or c > 0xBF then return false end
        point = (point << 6) | (c & 0x3F)
      end
      if rem == 1 and point < 0x80 then return false end
      if rem == 2 and (point < 0x800 or (point >= 0xD800 and point <= 0xDFFF)) then return false end
      if rem == 3 and (point < 0x10000 or point > 0x10FFFF) then return false end
    end
  end
  return true
end
`

// funcName maps a declaration reference to one of its generated functions.
func funcName(m *Module, prefix, decl string) string {
	ref := m.Require(decl)
	i := lastDot(ref)
	return ref[:i+1] + prefix + ref[i+1:]
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return 0
}

// call emits the C++-style early-out around one serialize call: on one line
// as StyLua keeps a simple conditional (Format expands it when it does not
// fit), and as a block when it carries a comment, which StyLua never collapses.
func (m *Module) call(ind, expr, comment string) {
	if comment == "" {
		m.Printf("%sif not %s then return false end\n", ind, expr)
		return
	}
	m.Printf("%sif not %s then%s\n%s  return false\n%send\n", ind, expr, comment, ind, ind)
}

func emitReadStruct(m *Module, d *ir.Struct) {
	m.Printf("-- Zero%s resets the §5 ZERO form: all-zero storage, without declared defaults.\n", d.Name)
	m.Printf("function _M.Zero%s(%s)\n", d.Name, unusedIf(len(d.Fields) == 0, "value"))
	if len(d.Fields) == 0 {
		m.Printf("  -- empty body — nothing to reset (SPEC §4.6)\n")
	}
	for _, f := range d.Fields {
		emitZeroField(m, f, "value", "  ")
	}
	m.Printf("end\n\n")

	m.Printf("-- Read%s reads the type wire (SPEC §4.3, §6.1); false refuses without a latch\n", d.Name)
	m.Printf("-- (a stream overflow rides stream:error()).\n")
	m.Printf("function _M.Read%s(%s, %s)\n", d.Name, unusedIf(len(d.Items) == 0, "stream"), unusedIf(!itemsTouchValue(d.Items), "value"))
	if len(d.Items) == 0 {
		m.Printf("  -- empty body — presence is the payload (SPEC §4.6)\n")
	}
	emitReadItems(m, d.Items, "  ")
	m.Printf("  return true\nend\n\n")
}

func emitReadUnion(m *Module, d *ir.Union) {
	m.Printf("-- Zero%s resets the union to None; unselected arms are unspecified (SPEC §4.8).\n", d.Name)
	m.Printf("function _M.Zero%s(value)\n  value.Type = 0\nend\n\n", d.Name)
	m.Printf("function _M.Read%s(%s, value)\n", d.Name, unusedIf(d.Max == 0, "stream"))
	if d.Max == 0 {
		m.Printf("  value.Type = 0 -- zero wire bits — only None exists (SPEC §4.8)\n")
		m.Printf("  return true\nend\n\n")
		return
	}
	m.call("  ", fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", d.Max), " -- rejects a tag above the count (SPEC §4.8)")
	m.Printf("  value.Type = BITS.value\n")
	for _, v := range d.Variants {
		m.Printf("  if value.Type == %s then\n", tagValue(m, d, v))
		if v.Void() {
			m.Printf("    return true -- a payload-free arm: the tag is the whole wire (SPEC §4.8)\n")
			m.Printf("  end\n")
			continue
		}
		arm := tableKey(ir.GoExportName(v.Name))
		m.Printf("    value.%s = %s -- fresh declared defaults on every selection\n", arm, constructor(m, v.Type)+"()")
		m.Printf("    return %s(stream, value.%s)\n", funcName(m, "Read", v.Type), arm)
		m.Printf("  end\n")
	}
	m.Printf("  return true -- None\nend\n\n")
}

// tagValue is the tag enum member for a union variant, required from the
// union's own module or another file.
func tagValue(m *Module, d *ir.Union, v ir.UnionVariant) string {
	return key(m.Require(d.Name+"Type"), ir.GoExportName(v.Name))
}

func emitReadItems(m *Module, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(m, item.F, ind)
		case *ir.ConstItem:
			emitConstItem(m, item, ind)
		case *ir.ReservedItem:
			emitReservedItem(m, item, ind)
		case *ir.AlignItem:
			m.call(ind, "stream:serializeAlign()", " -- rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "not "
			}
			m.Printf("%sif %svalue.%s then\n", ind, neg, ir.GoExportName(item.Cond))
			emitReadItems(m, item.Then, ind+"  ")
			emitZeroItems(m, item.Else, "value", ind+"  ")
			m.Printf("%selse\n", ind)
			if item.Else != nil {
				emitReadItems(m, item.Else, ind+"  ")
			}
			emitZeroItems(m, item.Then, "value", ind+"  ")
			m.Printf("%send\n", ind)
		}
	}
}

func emitConstItem(m *Module, item *ir.ConstItem, ind string) {
	holder, fn := "BITS", "serializeBits"
	if item.Bits > 32 {
		holder, fn = "BIG", "serializeBits64"
	}
	m.call(ind, fmt.Sprintf("stream:%s(%s, %d)", fn, holder, item.Bits), "")
	m.Printf("%sif %s.value ~= %s then -- const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, holder, IntLiteral(item.Value), item.Value.String(), item.Bits)
	m.Printf("%s  return false\n%send\n", ind, ind)
}

func emitReservedItem(m *Module, item *ir.ReservedItem, ind string) {
	holder, fn := "BITS", "serializeBits"
	if item.Bits > 32 {
		holder, fn = "BIG", "serializeBits64"
	}
	m.call(ind, fmt.Sprintf("stream:%s(%s, %d)", fn, holder, item.Bits), "")
	m.Printf("%sif %s.value ~= 0 then -- reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, holder, item.Bits)
	m.Printf("%s  return false\n%send\n", ind, ind)
}

func emitReadField(m *Module, f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := IntLiteral(big.NewInt(f.ArrayBound))
		limit := bound
		if f.Array == ir.ArrayCounted {
			m.call(ind, fmt.Sprintf("stream:serializeInt(BITS, %d, %s)", f.ArrayMin, bound),
				" -- the count guards the loop (SPEC §6.3)")
			m.Printf("%s%sCount = BITS.value\n", ind, name)
			limit = name + "Count"
		}
		m.Printf("%sfor i = 1, %s do\n", ind, limit)
		emitReadScalar(m, f, name+"[i]", ind+"  ")
		m.Printf("%send\n", ind)
		return
	}
	emitReadScalar(m, f, name, ind)
}

func emitReadScalar(m *Module, f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			m.Printf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, rawLiteral(f.Type, rawMin))
			return
		}
		holder := "BITS"
		if f.Type.Width > 64 {
			holder = "WIDE"
		} else if f.Type.Width > 32 {
			holder = "BIG"
		}
		m.call(ind, fmt.Sprintf("stream:serializeFixed(%s, %d, %d, %s, %s)", holder,
			f.Type.IntBits, f.Type.FracBits, rawLiteral(f.Type, f.IntMin), rawLiteral(f.Type, f.IntMax)), "")
		m.Printf("%s%s = %s.value\n", ind, name, holder)
	case ir.TInt:
		if f.Type.Width == 128 {
			holder := "WIDE"
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					m.Printf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, Int128Literal(f.IntMin))
					return
				}
				m.call(ind, fmt.Sprintf("stream:serializeInt128(%s, %s, %s)", holder,
					Int128Literal(f.IntMin), Int128Literal(f.IntMax)), "")
			} else {
				m.call(ind, fmt.Sprintf("stream:serializeUint128(%s)", holder), "")
			}
			m.Printf("%s%s = %s.value\n", ind, name, holder)
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				m.Printf("%s%s = %s -- degenerate range: zero bits (SPEC §4.6)\n", ind, name, IntLiteral(f.IntMin))
				return
			}
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				m.call(ind, fmt.Sprintf("stream:serializeInt(BITS, %s, %s)", IntLiteral(f.IntMin), IntLiteral(f.IntMax)), "")
				m.Printf("%s%s = BITS.value\n", ind, name)
			case "int64":
				m.call(ind, fmt.Sprintf("stream:serializeInt64(BIG, %s, %s)", IntLiteral(f.IntMin), IntLiteral(f.IntMax)), "")
				m.Printf("%s%s = BIG.value\n", ind, name)
			default:
				bits := ir.BitsRequired(f.IntMin, f.IntMax)
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				m.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", bits), "")
				if diff.Cmp(maxUint64) != 0 {
					if diff.Cmp(maxInt64) > 0 {
						m.Printf("%sif math.ult(%s, BIG.value) then -- a read rejects out-of-range (SPEC §5)\n", ind, IntLiteral(diff))
					} else {
						m.Printf("%sif BIG.value > %s then -- a read rejects out-of-range (SPEC §5)\n", ind, IntLiteral(diff))
					}
					m.Printf("%s  return false\n%send\n", ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					m.Printf("%s%s = BIG.value\n", ind, name)
				} else {
					m.Printf("%s%s = BIG.value + %s\n", ind, name, IntLiteral(f.IntMin))
				}
			}
			return
		}
		emitReadBareInt(m, f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			m.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
			m.Printf("%s%s = BITS.value\n", ind, name)
		} else {
			m.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", f.Type.Width), "")
			m.Printf("%s%s = BIG.value\n", ind, name)
		}
	case ir.TBool:
		m.call(ind, "stream:serializeBool(BOOL)", "")
		m.Printf("%s%s = BOOL.value\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			m.call(ind, fmt.Sprintf("stream:serializeCompressedFloat(BYTES, %s, %s, %s)",
				FloatLiteral(f.FMin), FloatLiteral(f.FMax), FloatLiteral(f.Resolution)), "")
			m.Printf("%s%s = BYTES.value\n", ind, name)
			return
		}
		m.call(ind, "stream:serializeFloat(BYTES)", "")
		m.Printf("%s%s = BYTES.value\n", ind, name)
	case ir.TFloat64:
		m.call(ind, "stream:serializeDouble(BYTES)", "")
		m.Printf("%s%s = BYTES.value\n", ind, name)
	case ir.TWString:
		emitReadWString(m, f, name, ind)
	case ir.TString, ir.TBytes:
		// Each scalar gets its own Lua scope: adjacent string fields both need len.
		m.Printf("%sdo\n", ind)
		ind += "  "
		m.call(ind, fmt.Sprintf("stream:serializeInt(BITS, 0, %s)", IntLiteral(big.NewInt(f.Type.Size))),
			" -- the length guards the payload (§6.3)")
		m.Printf("%slocal len = BITS.value\n", ind)
		m.call(ind, "stream:serializeBytes(BYTES, len)", "")
		m.Printf("%s%s = BYTES.value\n", ind, name)
		if f.Type.Kind == ir.TString {
			m.Printf("%sif not utf8valid(%s) then return false end -- malformed UTF-8 (SPEC §4.7)\n", ind, name)
			m.Printf("%sif string.find(%s, \"\\0\", 1, true) then return false end -- interior null (SPEC §4.7)\n", ind, name)
		}
		m.Printf("%send\n", ind[:len(ind)-2])
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			m.call(ind, fmt.Sprintf("stream:serializeInt(BITS, 0, %d)", ref.Max), "")
			m.Printf("%s%s = BITS.value\n", ind, name)
		case *ir.Flags:
			if ref.WireBits <= 32 {
				m.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", ref.WireBits), "")
			} else {
				m.call(ind, fmt.Sprintf("stream:serializeBits64(BIG, %d)", ref.WireBits), "")
				m.Printf("%s%s = BIG.value\n", ind, name)
				return
			}
			m.Printf("%s%s = BITS.value\n", ind, name)
		case *ir.Struct, *ir.Union:
			m.call(ind, fmt.Sprintf("%s(stream, %s)", funcName(m, "Read", f.Type.Name), name), "")
		}
	}
}

func emitReadBareInt(m *Module, f *ir.Field, name, ind string) {
	switch {
	case f.Type.Width == 64:
		m.call(ind, "stream:serializeBits64(BIG, 64)", "")
		m.Printf("%s%s = BIG.value\n", ind, name)
	case !f.Type.Signed && f.Type.Width == 32:
		m.call(ind, "stream:serializeBits(BITS, 32)", "")
		m.Printf("%s%s = BITS.value\n", ind, name)
	case f.Type.Signed:
		// A signed scalar's raw temporary must not collide with its neighbour.
		m.Printf("%sdo\n", ind)
		ind += "  "
		m.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
		// Lua's >> is logical; recover the sign through the same-width
		// unsigned round trip (SPEC §4.3).
		m.Printf("%slocal raw = BITS.value\n", ind)
		m.Printf("%sif raw >= %s then raw = raw - %s end\n", ind,
			IntLiteral(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width-1))),
			IntLiteral(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width))))
		m.Printf("%s%s = raw\n", ind, name)
		m.Printf("%send\n", ind[:len(ind)-2])
	default:
		m.call(ind, fmt.Sprintf("stream:serializeBits(BITS, %d)", f.Type.Width), "")
		m.Printf("%s%s = BITS.value\n", ind, name)
	}
}

func emitReadWString(m *Module, f *ir.Field, name, ind string) {
	// Keep len and the surrogate-state local to this one field.
	m.Printf("%sdo\n", ind)
	ind += "  "
	m.call(ind, fmt.Sprintf("stream:serializeInt(BITS, 0, %s)", IntLiteral(big.NewInt(f.Type.Size))),
		" -- the length guards the payload (§6.3)")
	m.Printf("%slocal len = BITS.value\n", ind)
	m.Printf("%s%s = {}\n", ind, name)
	m.Printf("%slocal expectLow = false\n", ind)
	m.Printf("%sfor i = 1, len do\n", ind)
	m.call(ind+"  ", "stream:serializeBits(BITS, 32)", "")
	m.Printf("%s  local group = BITS.value\n", ind)
	m.Printf("%s  if group == 0 or group > 0xFFFF then return false end -- UTF-16 code unit (SPEC §4.12)\n", ind)
	m.Printf("%s  local high = group >= 0xD800 and group <= 0xDBFF\n", ind)
	m.Printf("%s  local low = group >= 0xDC00 and group <= 0xDFFF\n", ind)
	m.Printf("%s  if low ~= expectLow then return false end\n", ind)
	m.Printf("%s  expectLow = high\n", ind)
	m.Printf("%s  %s[i] = group\n", ind, name)
	m.Printf("%send\n", ind)
	m.Printf("%sif expectLow then return false end\n", ind)
	m.Printf("%send\n", ind[:len(ind)-2])
}

// rawLiteral renders a fixed-point raw value: a Lua integer at 64 bits or
// fewer, a { lo, hi } pair at 128.
func rawLiteral(t ir.FieldType, v *big.Int) string {
	if t.Width > 64 {
		return Int128Literal(v)
	}
	return IntLiteral(v)
}

func emitZeroItems(m *Module, items []ir.Item, path, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitZeroField(m, item.F, path, ind)
		case *ir.Branch:
			emitZeroItems(m, item.Then, path, ind)
			emitZeroItems(m, item.Else, path, ind)
		}
	}
}

func emitZeroField(m *Module, f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		m.Printf("%s%s = \"\"\n", ind, name)
	case f.Type.Kind == ir.TWString:
		m.Printf("%s%s = {}\n", ind, name)
	case f.Array != ir.ArrayNone:
		if f.Type.Kind == ir.TNamed {
			if _, ok := f.Type.Ref.(*ir.Struct); ok {
				m.Printf("%sfor i = 1, %s do\n%s  %s(%s[i])\n%send\n", ind, IntLiteral(big.NewInt(f.ArrayBound)),
					ind, funcName(m, "Zero", f.Type.Name), name, ind)
			} else if _, ok := f.Type.Ref.(*ir.Union); ok {
				m.Printf("%sfor i = 1, %s do\n%s  %s(%s[i])\n%send\n", ind, IntLiteral(big.NewInt(f.ArrayBound)),
					ind, funcName(m, "Zero", f.Type.Name), name, ind)
			} else {
				m.Printf("%sfor i = 1, %s do\n%s  %s[i] = %s\n%send\n", ind, IntLiteral(big.NewInt(f.ArrayBound)), ind, name, ZeroValue(m, f.Type), ind)
			}
		} else {
			m.Printf("%sfor i = 1, %s do\n%s  %s[i] = %s\n%send\n", ind, IntLiteral(big.NewInt(f.ArrayBound)), ind, name, ZeroValue(m, f.Type), ind)
		}
		if f.Array == ir.ArrayCounted {
			m.Printf("%s%sCount = 0\n", ind, name)
		}
	case f.Type.Kind == ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			m.Printf("%s%s(%s)\n", ind, funcName(m, "Zero", f.Type.Name), name)
		case *ir.Union:
			m.Printf("%s%s(%s)\n", ind, funcName(m, "Zero", f.Type.Name), name)
		default:
			m.Printf("%s%s = %s\n", ind, name, ZeroValue(m, f.Type))
		}
	default:
		m.Printf("%s%s = %s\n", ind, name, ZeroValue(m, f.Type))
	}
}

// intRangePath picks the serialize.lua call family for a ranged integer —
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

// unusedIf spells a parameter the function body never reads with luacheck's
// "deliberately unread" prefix (an underscore), so the generated Lua is clean
// under the strict lint (make/schema-lua-generated-luacheck-gate.mk) without
// an ignore: every reader, writer and reset keeps its one fixed signature.
func unusedIf(unused bool, name string) string {
	if unused {
		return "_" + name
	}
	return name
}

// itemsTouchValue reports whether a wire tree reads or writes the value: any
// field does, and any branch does (its condition is a field of the value).
// A tree of const, reserved and align items alone moves only the stream.
func itemsTouchValue(items []ir.Item) bool {
	for _, item := range items {
		switch item.(type) {
		case *ir.FieldItem, *ir.Branch:
			return true
		}
	}
	return false
}
