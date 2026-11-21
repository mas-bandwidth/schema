package ruby

import (
	"fmt"
	"math/big"
	"path"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// GenerateDescriptor emits the D1 alternative packet codec shape: a frozen
// per-type descriptor table walked by a generic pure-Ruby interpreter
// (docs/PORT-RUBY.md, D1), used to benchmark straight-line code with bound
// locals against a descriptor walk with YJIT on and off over the same
// byte-checked 64-variant BenchMixed workload.
func GenerateDescriptor(u *ir.Unit) (map[string][]byte, error) {
	if err := CheckNames(u); err != nil {
		return nil, err
	}
	if err := refuseDeclarations(u); err != nil {
		return nil, err
	}
	if err := checkCarried(u); err != nil {
		return nil, err
	}
	mod := ModuleName(u.Package)
	pkg := u.Package
	out, err := Generate(u)
	if err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		out[path.Join(pkg, f.Base+"."+ext)] = []byte(emitDescriptorModule(u, f, mod))
	}
	schemaPath := path.Join(pkg, RuntimeBase+"."+ext)
	schemaStr := strings.TrimSuffix(string(out[schemaPath]), "end\n")
	out[schemaPath] = []byte(schemaStr + descriptorWalkerRuntime() + "end\n")
	return out, nil
}

func emitDescriptorModule(u *ir.Unit, f *ir.File, mod string) string {
	m := &rbModule{
		u:           u,
		f:           f,
		mod:         mod,
		surfaceOnly: true,
	}
	for _, d := range ir.EmissionOrder(f) {
		switch d := d.(type) {
		case *ir.Const:
			m.emitConst(d)
		case *ir.Enum:
			m.emitEnum(d)
		case *ir.Flags:
			m.emitFlags(d)
		case *ir.Struct:
			m.emitDescriptorStruct(d)
		case *ir.Union:
			m.emitDescriptorUnion(d)
		}
	}
	return m.assemble()
}

func (m *rbModule) emitDescriptorStruct(d *ir.Struct) {
	m.emitStruct(d)
	// Replace trailing "  end\n\n" of emitStruct with descriptor codec + "  end\n\n".
	cur := strings.TrimSuffix(m.body.String(), "  end\n\n")
	m.body.Reset()
	m.body.WriteString(cur)
	m.line("")
	m.line("    S_DESC = %s", m.descItemsExpr(structItems(d)))
	m.line("")
	m.emitEntryPoints(d.Name)
	m.line("    def _s_read(stream)")
	m.line("      ::%s._s_desc_read_items(self, S_DESC, stream)", m.mod)
	m.line("    end")
	m.line("")
	m.line("    def _s_write(stream)")
	m.line("      ::%s._s_desc_write_items(self, S_DESC, stream)", m.mod)
	m.line("    end")
	m.line("")
	m.emitValidateStruct(d)
	m.line("    def _s_zero")
	m.line("      ::%s._s_desc_zero_items(self, S_DESC)", m.mod)
	m.line("    end")
	m.line("  end")
	m.line("")
}

func (m *rbModule) emitDescriptorUnion(d *ir.Union) {
	m.emitUnion(d)
	cur := strings.TrimSuffix(m.body.String(), "  end\n\n")
	m.body.Reset()
	m.body.WriteString(cur)
	m.line("")
	var arms []string
	for _, v := range d.Variants {
		if v.Void() {
			arms = append(arms, "nil")
			continue
		}
		arms = append(arms, fmt.Sprintf("[%[1]s, %[2]s, -> { %[3]s }].freeze",
			fieldSymbol(v.Name), m.descFieldSpec(v.F), m.unionArmDefault(v)))
	}
	m.line("    S_UNION_DESC = [%d, [%s].freeze].freeze", d.Max, strings.Join(arms, ", "))
	m.line("")
	m.emitEntryPoints(d.Name)
	m.line("    def _s_read(stream)")
	m.line("      ::%s._s_desc_read_union(self, S_UNION_DESC, stream)", m.mod)
	m.line("    end")
	m.line("")
	m.line("    def _s_write(stream)")
	m.line("      ::%s._s_desc_write_union(self, S_UNION_DESC, stream)", m.mod)
	m.line("    end")
	m.line("")
	m.emitValidateUnion(d)
	m.emitZeroUnion(d)
	m.line("  end")
	m.line("")
}

func (m *rbModule) descItemsExpr(items []ir.Item) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			parts = append(parts, fmt.Sprintf("[:field, %s, %s].freeze", fieldSymbol(item.F.Name), m.descFieldSpec(item.F)))
		case *ir.AlignItem:
			parts = append(parts, "[:align].freeze")
		case *ir.ConstItem:
			parts = append(parts, fmt.Sprintf("[:const, %d, %s].freeze", item.Bits, intLiteral(item.Value)))
		case *ir.ReservedItem:
			parts = append(parts, fmt.Sprintf("[:reserved, %d].freeze", item.Bits))
		case *ir.Branch:
			parts = append(parts, fmt.Sprintf("[:branch, %s, %t, %s, %s].freeze",
				fieldSymbol(item.Cond), item.Neg, m.descItemsExpr(item.Then), m.descItemsExpr(item.Else)))
		}
	}
	return "[" + strings.Join(parts, ", ") + "].freeze"
}

func (m *rbModule) descFieldSpec(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		elem := arrayElem(f)
		min := f.ArrayBound
		counted := false
		if f.Array == ir.ArrayCounted {
			min = f.ArrayMin
			counted = true
		}
		return fmt.Sprintf("[:array, %t, %d, %d, %s, -> { %s }].freeze",
			counted, min, f.ArrayBound, m.descFieldSpec(elem), m.defaultExpr(elem))
	}
	switch fieldKind(f) {
	case "bool":
		return "[:bool].freeze"
	case "int", "int128":
		if f.HasIntRange {
			return fmt.Sprintf("[:range, :%s, %s, %s].freeze", rangeFn(f.IntMin, f.IntMax), intLiteral(f.IntMin), intLiteral(f.IntMax))
		}
		if f.Type.Width == 128 {
			return "[:uint128].freeze"
		}
		return fmt.Sprintf("[:int_bits, %d, %t].freeze", f.Type.Width, f.Type.Signed)
	case "bits":
		return fmt.Sprintf("[:bits, %d].freeze", f.Type.Width)
	case "enum":
		ref := f.Type.Ref.(*ir.Enum)
		return fmt.Sprintf("[:range, :%s, 0, %d].freeze", rangeFn(big.NewInt(0), big.NewInt(ref.Max)), ref.Max)
	case "flags":
		ref := f.Type.Ref.(*ir.Flags)
		return fmt.Sprintf("[:bits, %d].freeze", ref.WireBits)
	case "fixed":
		return fmt.Sprintf("[:fixed, %d, %d, %s, %s].freeze", f.Type.IntBits, f.Type.FracBits, intLiteral(f.IntMin), intLiteral(f.IntMax))
	case "float32":
		if f.HasFloatRange {
			steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			return fmt.Sprintf("[:cfloat, %d, %d, %s, %s].freeze", steps, bits, compressedDelta(f), compressedMin(f))
		}
		return "[:float32].freeze"
	case "float64":
		return "[:float64].freeze"
	case "string":
		return fmt.Sprintf("[:string, %d].freeze", f.Type.Size)
	case "bytes":
		return fmt.Sprintf("[:bytes, %d].freeze", f.Type.Size)
	case "type", "union":
		return fmt.Sprintf("[:object, -> { %s.new }].freeze", f.Type.Name)
	}
	return "[:unknown].freeze"
}

func descriptorWalkerRuntime() string {
	return `
  def self._s_desc_read_val(spec, cur, stream)
    ri = Serialize::Ref.new(0)
    case spec[0]
    when :bool
      rb = Serialize::Ref.new(false)
      return [nil, false] unless stream.serialize_bool(rb)
      [rb.value, true]
    when :range
      min = spec[2]
      max = spec[3]
      return [min, true] if min == max
      return [nil, false] unless stream.public_send(spec[1], ri, min, max)
      [ri.value, true]
    when :int_bits
      w = spec[1]
      return [0, true] if w == 0
      return [nil, false] unless stream.serialize_bits(ri, w)
      v = ri.value
      if spec[2] && v >= (1 << (w - 1))
        v -= (1 << w)
      end
      [v, true]
    when :bits
      w = spec[1]
      return [0, true] if w == 0
      return [nil, false] unless stream.serialize_bits(ri, w)
      [ri.value, true]
    when :uint128
      return [nil, false] unless stream.serialize_uint128(ri)
      [ri.value, true]
    when :fixed
      return [nil, false] unless stream.serialize_fixed(ri, spec[1], spec[2], spec[3], spec[4])
      [ri.value, true]
    when :float32
      rf = Serialize::Ref.new(0.0)
      return [nil, false] unless stream.serialize_float(rf)
      [rf.value, true]
    when :cfloat
      rf = Serialize::Ref.new(0.0)
      return [nil, false] unless stream.serialize_compressed_float_precomputed(rf, spec[1], spec[2], spec[3], spec[4])
      [rf.value, true]
    when :float64
      rf = Serialize::Ref.new(0.0)
      return [nil, false] unless stream.serialize_double(rf)
      [rf.value, true]
    when :string
      rs = Serialize::Ref.new("")
      return [nil, false] unless stream.serialize_string(rs, spec[1] + 1)
      [rs.value, true]
    when :bytes
      rby = Serialize::Ref.new("".b)
      return [nil, false] unless stream.serialize_int(ri, 0, spec[1])
      return [nil, false] unless stream.serialize_bytes(rby, ri.value)
      [_s_copy_bytes(rby.value), true]
    when :object
      obj = cur || spec[1].call
      return [nil, false] unless obj._s_read(stream)
      [obj, true]
    when :array
      counted = spec[1]
      min = spec[2]
      max = spec[3]
      elem_spec = spec[4]
      mk = spec[5]
      if counted
        if min == max
          cnt = min
        else
          return [nil, false] unless stream.serialize_int(ri, min, max)
          cnt = ri.value
        end
        arr = ::Array.new(cnt) { mk.call }
      else
        arr = cur
      end
      i = 0
      n = arr.length
      while i < n
        ev, ok = _s_desc_read_val(elem_spec, arr[i], stream)
        return [nil, false] unless ok
        arr[i] = ev
        i += 1
      end
      [arr, true]
    else
      [nil, false]
    end
  end

  def self._s_desc_write_val(spec, val, stream)
    ri = Serialize::Ref.new(0)
    case spec[0]
    when :bool
      stream.serialize_bool(Serialize::Ref.new(val))
    when :range
      min = spec[2]
      max = spec[3]
      if min != max
        ri.value = val
        stream.public_send(spec[1], ri, min, max)
      end
    when :int_bits
      w = spec[1]
      if w > 0
        ri.value = spec[2] ? (val & ((1 << w) - 1)) : val
        stream.serialize_bits(ri, w)
      end
    when :bits
      w = spec[1]
      if w > 0
        ri.value = val
        stream.serialize_bits(ri, w)
      end
    when :uint128
      ri.value = val
      stream.serialize_uint128(ri)
    when :fixed
      ri.value = val
      stream.serialize_fixed(ri, spec[1], spec[2], spec[3], spec[4])
    when :float32
      stream.serialize_float(Serialize::Ref.new(val))
    when :cfloat
      stream.serialize_compressed_float_precomputed(Serialize::Ref.new(val), spec[1], spec[2], spec[3], spec[4])
    when :float64
      stream.serialize_double(Serialize::Ref.new(val))
    when :string
      stream.serialize_string(Serialize::Ref.new(val), spec[1] + 1)
    when :bytes
      ri.value = val.bytesize
      stream.serialize_int(ri, 0, spec[1])
      stream.serialize_bytes(Serialize::Ref.new(val), ri.value)
    when :object
      val._s_write(stream)
    when :array
      if spec[1] && spec[2] != spec[3]
        ri.value = val.length
        stream.serialize_int(ri, spec[2], spec[3])
      end
      elem_spec = spec[4]
      val.each { |e| _s_desc_write_val(elem_spec, e, stream) }
    end
    nil
  end

  def self._s_desc_zero_val(spec, cur)
    case spec[0]
    when :bool then false
    when :float32, :cfloat, :float64 then 0.0
    when :string then ""
    when :bytes then "".b.freeze
    when :object
      cur._s_zero
      cur
    when :array
      return [] if spec[1]
      elem_spec = spec[4]
      ::Array.new(spec[3]) { |i| _s_desc_zero_val(elem_spec, cur[i]) }
    else
      0
    end
  end

  def self._s_desc_read_items(obj, items, stream)
    ri = Serialize::Ref.new(0)
    items.each do |item|
      case item[0]
      when :field
        sym = item[1]
        v, ok = _s_desc_read_val(item[2], obj.public_send(sym), stream)
        return false unless ok
        obj.public_send(:"#{sym}=", v)
      when :align
        return false unless stream.serialize_align
      when :const
        return false unless stream.serialize_bits(ri, item[1])
        return false if ri.value != item[2]
      when :reserved
        return false unless stream.serialize_bits(ri, item[1])
        return false if ri.value != 0
      when :branch
        cond = obj.public_send(item[1])
        cond = !cond if item[2]
        if cond
          return false unless _s_desc_read_items(obj, item[3], stream)
          _s_desc_zero_items(obj, item[4])
        else
          return false unless _s_desc_read_items(obj, item[4], stream)
          _s_desc_zero_items(obj, item[3])
        end
      end
    end
    true
  end

  def self._s_desc_write_items(obj, items, stream)
    ri = Serialize::Ref.new(0)
    items.each do |item|
      case item[0]
      when :field
        _s_desc_write_val(item[2], obj.public_send(item[1]), stream)
      when :align
        stream.serialize_align
      when :const
        ri.value = item[2]
        stream.serialize_bits(ri, item[1])
      when :reserved
        ri.value = 0
        stream.serialize_bits(ri, item[1])
      when :branch
        cond = obj.public_send(item[1])
        cond = !cond if item[2]
        _s_desc_write_items(obj, cond ? item[3] : item[4], stream)
      end
    end
    nil
  end

  def self._s_desc_zero_items(obj, items)
    items.each do |item|
      case item[0]
      when :field
        sym = item[1]
        obj.public_send(:"#{sym}=", _s_desc_zero_val(item[2], obj.public_send(sym)))
      when :branch
        _s_desc_zero_items(obj, item[3])
        _s_desc_zero_items(obj, item[4])
      end
    end
    nil
  end

  def self._s_desc_read_union(obj, udesc, stream)
    max = udesc[0]
    if max == 0
      obj.type = 0
      return true
    end
    ri = Serialize::Ref.new(0)
    return false unless stream.serialize_int(ri, 0, max)
    tag = ri.value
    obj.type = tag
    return true if tag == 0
    arm = udesc[1][tag - 1]
    return true if arm.nil?
    sym = arm[0]
    fresh = arm[2].call
    obj.public_send(:"#{sym}=", fresh)
    v, ok = _s_desc_read_val(arm[1], fresh, stream)
    return false unless ok
    obj.public_send(:"#{sym}=", v)
    true
  end

  def self._s_desc_write_union(obj, udesc, stream)
    max = udesc[0]
    return nil if max == 0
    ri = Serialize::Ref.new(obj.type)
    stream.serialize_int(ri, 0, max)
    return nil if obj.type == 0
    arm = udesc[1][obj.type - 1]
    return nil if arm.nil?
    _s_desc_write_val(arm[1], obj.public_send(arm[0]), stream)
    nil
  end
`
}
