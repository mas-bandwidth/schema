package ruby

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

func (m *rbModule) emitConst(d *ir.Const) {
	value := ""
	if d.IsFloat {
		if d.Storage == "float32" {
			value = floatLiteral(float32Value(d.Float))
		} else {
			value = floatLiteral(d.Float)
		}
	} else {
		value = intLiteral(d.Int)
	}
	note := ""
	if d.Expr != nil {
		if r := ir.RenderExpr(d.Expr); r != value {
			note = " # = " + r
		}
	}
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "  ", "#"))
	}
	m.line("  %s = %s%s", d.Name, value, note)
	m.line("")
}

func (m *rbModule) emitEnum(d *ir.Enum) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "  ", "#"))
	}
	m.line("  module %s", d.Name)
	m.line("    None = 0")
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) && d.VariantDocs[i] != "" {
			m.printf("%s", docComment(d.VariantDocs[i], "    ", "#"))
		}
		m.line("    %s = %d", VariantConstant(v), i+1)
	}
	m.line("    Max = %d", d.Max)
	m.printf("    NAMES = { 0 => \"None\"")
	for i, v := range d.Variants {
		m.printf(", %d => %s", i+1, rubyStringLiteral(v))
	}
	m.printf(" }.freeze\n")
	m.line("  end")
	m.line("")
}

func (m *rbModule) emitFlags(d *ir.Flags) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "  ", "#"))
	}
	m.line("  module %s", d.Name)
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) && d.VariantDocs[i] != "" {
			m.printf("%s", docComment(d.VariantDocs[i], "    ", "#"))
		}
		m.line("    %s = 1 << %d", VariantConstant(v), i)
	}
	m.line("    Count = %d", len(d.Variants))
	if len(d.Variants) == 0 {
		m.line("    NAMES = {}.freeze #: ::Hash[Integer, String]")
	} else {
		m.printf("    NAMES = { ")
		for i, v := range d.Variants {
			if i > 0 {
				m.printf(", ")
			}
			m.printf("%d => %s", i, rubyStringLiteral(v))
		}
		m.printf(" }.freeze\n")
	}
	m.line("  end")
	m.line("")
}

func (m *rbModule) emitStruct(d *ir.Struct) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "  ", "#"))
	}
	m.line("  class %s", d.Name)
	if len(d.Fields) > 0 {
		syms := make([]string, len(d.Fields))
		hasObjectID := false
		for i, f := range d.Fields {
			syms[i] = fieldSymbol(f.Name)
			if f.Name == "object_id" {
				hasObjectID = true
			}
		}
		if hasObjectID {
			m.line("    _s_verbose = $VERBOSE")
			m.line("    $VERBOSE = nil")
			m.line("    attr_accessor %s", strings.Join(syms, ", "))
			m.line("    $VERBOSE = _s_verbose")
		} else {
			m.line("    attr_accessor %s", strings.Join(syms, ", "))
		}
		m.line("")
	}
	m.line("    def initialize")
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				m.line("      # %s — wire branch; storage holds both sides (SPEC §5)", f.Guard)
			}
			prevGuard = f.Guard
		}
		if f.Doc != "" {
			m.printf("%s", docComment(f.Doc, "      ", "#"))
		}
		val := m.defaultExpr(f)
		if f.Array != ir.ArrayNone {
			val = m.arrayDefault(f)
		}
		m.line("      %s = %s", fieldIvar(f.Name), val)
	}
	m.line("    end")
	m.line("")
	m.emitCopyFrom(d.Name, d.Fields)
	m.emitToH(d.Fields)
	m.emitEquality(d.Name, d.Fields)
	m.emitInspect(d.Name, d.Fields)
	if !m.surfaceOnly {
		m.emitCodecStruct(d)
	}
	m.line("  end")
	m.line("")
}

func (m *rbModule) emitUnion(d *ir.Union) {
	tag := d.Name + "Type"
	m.line("  module %s", tag)
	m.line("    None = 0")
	for _, v := range d.Variants {
		m.line("    %s = %d", ir.GoExportName(v.Name), tagOf(d, v))
	}
	m.line("    Max = %d", d.Max)
	m.printf("    NAMES = { 0 => \"None\"")
	for i, v := range d.Variants {
		m.printf(", %d => %s", i+1, rubyStringLiteral(v.Name))
	}
	m.printf(" }.freeze\n")
	m.line("  end")
	m.line("")

	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "  ", "#"))
	}
	m.line("  class %s", d.Name)
	arms := unionArms(d)
	syms := []string{":type"}
	for _, v := range arms {
		syms = append(syms, fieldSymbol(v.Name))
	}
	m.line("    attr_accessor %s", strings.Join(syms, ", "))
	m.line("")
	m.line("    def initialize")
	m.line("      @type = 0")
	for _, v := range arms {
		if v.Doc != "" {
			m.printf("%s", docComment(v.Doc, "      ", "#"))
		}
		m.line("      %s = %s", fieldIvar(v.Name), m.unionArmDefault(v))
	}
	m.line("    end")
	m.line("")
	m.emitUnionCopyFrom(d, arms)
	m.emitUnionEquality(d, arms)
	m.emitUnionInspect(d, arms)
	if !m.surfaceOnly {
		m.emitCodecUnion(d)
	}
	m.line("  end")
	m.line("")
}

func unionArms(d *ir.Union) []ir.UnionVariant {
	out := make([]ir.UnionVariant, 0, len(d.Variants))
	for _, v := range d.Variants {
		if !v.Void() {
			out = append(out, v)
		}
	}
	return out
}

func tagOf(d *ir.Union, v ir.UnionVariant) int {
	for i, x := range d.Variants {
		if x.Name == v.Name {
			return i + 1
		}
	}
	return 0
}

func (m *rbModule) unionArmDefault(v ir.UnionVariant) string {
	if v.F == nil {
		return m.zeroExpr(&ir.Field{})
	}
	if v.F.Array != ir.ArrayNone {
		return m.arrayDefault(v.F)
	}
	return m.defaultExpr(v.F)
}

func (m *rbModule) arrayDefault(f *ir.Field) string {
	n := int64(0)
	switch f.Array {
	case ir.ArrayFixed:
		n = f.ArrayBound
	case ir.ArrayCounted:
		n = f.ArrayMin
	case ir.ArrayList:
		n = 0
	}
	return m.arrayFill(f, fmt.Sprint(n))
}

func (m *rbModule) arrayFill(f *ir.Field, countExpr string) string {
	elem := arrayElem(f)
	val := m.defaultExpr(elem)
	if isImmutableExpr(elem) {
		return fmt.Sprintf("::Array.new(%s, %s)", countExpr, val)
	}
	return fmt.Sprintf("::Array.new(%s) { %s }", countExpr, val)
}

func copyValueExpr(f *ir.Field, src string) string {
	if f.Array != ir.ArrayNone {
		elem := arrayElem(f)
		inner := copyValueExpr(elem, "_s_e")
		if inner == "_s_e" {
			return src + ".dup"
		}
		return fmt.Sprintf("%s.map { |_s_e| %s }", src, inner)
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return fmt.Sprintf("%s.new.%scopy_from(%s)", f.Type.Name, HelperPrefix, src)
		}
	}
	return src
}

func (m *rbModule) emitCopyFrom(name string, fields []*ir.Field) {
	m.line("    def %scopy_from(other)", HelperPrefix)
	if len(fields) == 0 {
		m.line("      _ = other")
	}
	for _, f := range fields {
		m.line("      %s", fieldWrite("self", f.Name, copyValueExpr(f, fieldRead("other", f.Name))))
	}
	m.line("      self")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitUnionCopyFrom(d *ir.Union, arms []ir.UnionVariant) {
	m.line("    def %scopy_from(other)", HelperPrefix)
	m.line("      self.type = other.type")
	for _, v := range arms {
		m.line("      %s", fieldWrite("self", v.Name, copyValueExpr(v.F, fieldRead("other", v.Name))))
	}
	m.line("      self")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitToH(fields []*ir.Field) {
	if len(fields) == 0 {
		m.line("    def to_h = {} #: ::Hash[Symbol, untyped]")
		m.line("")
		return
	}
	labels := make([]string, len(fields))
	for i, f := range fields {
		labels[i] = fieldLabel(f.Name, fieldIvar(f.Name))
	}
	m.line("    def to_h = { %s }", strings.Join(labels, ", "))
	m.line("")
}

func (m *rbModule) emitEquality(name string, fields []*ir.Field) {
	m.line("    def ==(other)")
	m.line("      return false unless other.instance_of?(%s)", name)
	if len(fields) == 0 {
		m.line("      true")
		m.line("    end")
		m.line("")
		return
	}
	m.line("      _s_o = other #: %s", name)
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = m.equalExpr(fieldRead("self", f.Name), fieldRead("_s_o", f.Name), f)
	}
	m.line("      %s", strings.Join(parts, " && "))
	m.line("    end")
	m.line("")
}

func (m *rbModule) equalExpr(a, b string, f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		elem := arrayElem(f)
		inner := m.equalExpr("_s_x", "_s_y", elem)
		if inner == "_s_x == _s_y" {
			return a + " == " + b
		}
		return fmt.Sprintf("(%s.length == %s.length && %s.each_index.all? { |_s_i| _s_x = %s[_s_i]; _s_y = %s[_s_i]; %s })", a, b, a, a, b, inner)
	}
	switch f.Type.Kind {
	case ir.TFloat32:
		return fmt.Sprintf("::%s._s_float32_bits_eq(%s, %s)", m.mod, a, b)
	case ir.TFloat64:
		return fmt.Sprintf("::%s._s_float64_bits_eq(%s, %s)", m.mod, a, b)
	}
	return a + " == " + b
}

func (m *rbModule) emitInspect(name string, fields []*ir.Field) {
	m.line("    def inspect")
	if len(fields) == 0 {
		m.line("      %q", name+"()")
		m.line("    end")
		m.line("")
		return
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("%s=#{%s.inspect}", f.Name, fieldRead("self", f.Name))
	}
	m.line("      \"%s(%s)\"", name, strings.Join(parts, ", "))
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitUnionEquality(d *ir.Union, arms []ir.UnionVariant) {
	m.line("    def ==(other)")
	m.line("      return false unless other.instance_of?(%s)", d.Name)
	m.line("      _s_o = other #: %s", d.Name)
	m.line("      return false if self.type != _s_o.type")
	for i, v := range arms {
		cond := "if"
		if i > 0 {
			cond = "elsif"
		}
		m.line("      %s self.type == %d", cond, tagOf(d, v))
		m.line("        return %s", m.equalExpr(fieldRead("self", v.Name), fieldRead("_s_o", v.Name), v.F))
	}
	if len(arms) > 0 {
		m.line("      end")
	}
	m.line("      true")
	m.line("    end")
	m.line("")
}

func (m *rbModule) emitUnionInspect(d *ir.Union, arms []ir.UnionVariant) {
	m.line("    def inspect")
	parts := []string{"type=#{self.type.inspect}"}
	for _, v := range arms {
		parts = append(parts, fmt.Sprintf("%s=#{%s.inspect}", v.Name, fieldRead("self", v.Name)))
	}
	m.line("      \"%s(%s)\"", d.Name, strings.Join(parts, ", "))
	m.line("    end")
	m.line("")
}

func docComment(doc, indent, marker string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(doc, "\n"), "\n") {
		fmt.Fprintf(&b, "%s%s %s\n", indent, marker, line)
	}
	return b.String()
}
