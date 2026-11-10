package python

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitConst emits a schema constant as a module-level Final (SPEC §4.2),
// with the author's expression in a trailing comment where it was folded.
func (m *pyModule) emitConst(d *ir.Const) {
	m.need("Final")
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
			note = "  # = " + r
		}
	}
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "", "#"))
	}
	m.line("%s: Final = %s%s", d.Name, value, note)
	m.line("")
}

// emitEnum emits an enum as a class of int constants: None_ = 0, the variants
// from 1, Max, and the name table the design asks for (SPEC §4.2).
func (m *pyModule) emitEnum(d *ir.Enum) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "", "#"))
	}
	m.line("class %s:", d.Name)
	m.line("    __slots__ = ()")
	m.line("    None_ = 0")
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) && d.VariantDocs[i] != "" {
			m.printf("%s", docComment(d.VariantDocs[i], "    ", "#"))
		}
		m.line("    %s = %d", v, i+1)
	}
	m.line("    Max = %d", d.Max)
	m.need("Final")
	m.printf("    NAMES: Final = {0: \"None\"")
	for i, v := range d.Variants {
		m.printf(", %d: %s", i+1, strconv.Quote(v))
	}
	m.printf("}\n\n")
}

// emitFlags emits a flags set as a class of mask constants and its name table
// (SPEC §4.2).
func (m *pyModule) emitFlags(d *ir.Flags) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "", "#"))
	}
	m.line("class %s:", d.Name)
	m.line("    __slots__ = ()")
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) && d.VariantDocs[i] != "" {
			m.printf("%s", docComment(d.VariantDocs[i], "    ", "#"))
		}
		m.line("    %s = 1 << %d", v, i)
	}
	m.line("    Count = %d", len(d.Variants))
	m.need("Final")
	m.printf("    NAMES: Final = {")
	for i, v := range d.Variants {
		if i > 0 {
			m.printf(", ")
		}
		m.printf("%d: %s", i, strconv.Quote(v))
	}
	m.printf("}\n\n")
}

// emitStruct emits one type's class: slots, the constructor at every field's
// declared default, equality by value and a readable repr, then its codec.
func (m *pyModule) emitStruct(d *ir.Struct) {
	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "", "#"))
	}
	m.line("class %s:", d.Name)
	if len(d.Fields) == 0 {
		m.line("    __slots__ = ()")
		m.line("")
		m.line("    def __init__(self) -> None:")
		m.line("        pass")
		m.line("")
		m.emitEquality(d.Name, nil)
		m.emitRepr(d.Name, nil)
		if !m.surfaceOnly {
			m.emitCodecStruct(d)
		}
		return
	}
	slots := make([]string, 0, len(d.Fields))
	for _, f := range d.Fields {
		slots = append(slots, f.Name)
	}
	m.line("    __slots__ = (%s)", quotedList(slots))
	m.line("")
	m.line("    def __init__(self) -> None:")
	for _, f := range d.Fields {
		value := m.defaultExpr(f)
		if f.Array != ir.ArrayNone {
			value = m.arrayDefault(f)
		}
		m.line("        self.%s = %s", f.Name, value)
	}
	m.line("")
	m.emitEquality(d.Name, d.Fields)
	m.emitRepr(d.Name, d.Fields)
	if !m.surfaceOnly {
		m.emitCodecStruct(d)
	}
}

// emitUnion emits the tag class and the union class. Construction is None;
// every payload arm is built at its own default (SPEC §4.8).
func (m *pyModule) emitUnion(d *ir.Union) {
	tag := d.Name + "Type"
	m.line("class %s:", tag)
	m.line("    __slots__ = ()")
	m.line("    None_ = 0")
	for _, v := range d.Variants {
		m.line("    %s = %d", ir.GoExportName(v.Name), tagOf(d, v))
	}
	m.line("    Max = %d", d.Max)
	m.need("Final")
	m.printf("    NAMES: Final = {0: \"None\"")
	for i, v := range d.Variants {
		m.printf(", %d: %s", i+1, strconv.Quote(v.Name))
	}
	m.printf("}\n\n")

	if d.Doc != "" {
		m.printf("%s", docComment(d.Doc, "", "#"))
	}
	m.line("class %s:", d.Name)
	arms := unionArms(d)
	slots := []string{"type"}
	for _, v := range arms {
		slots = append(slots, v.Name)
	}
	m.line("    __slots__ = (%s)", quotedList(slots))
	m.line("")
	m.line("    def __init__(self) -> None:")
	m.line("        self.type = 0")
	for _, v := range arms {
		m.line("        self.%s = %s", v.Name, m.unionArmDefault(v))
	}
	m.line("")
	m.emitUnionEquality(d, arms)
	m.emitUnionRepr(d, arms)
	if !m.surfaceOnly {
		m.emitCodecUnion(d)
	}
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

func (m *pyModule) unionArmDefault(v ir.UnionVariant) string {
	if v.F == nil {
		return m.zeroExpr(&ir.Field{})
	}
	if v.F.Array != ir.ArrayNone {
		return m.arrayDefault(v.F)
	}
	return m.defaultExpr(v.F)
}

func (m *pyModule) arrayDefault(f *ir.Field) string {
	elem := *f
	elem.Array = ir.ArrayNone
	value := m.defaultExpr(&elem)
	n := int64(0)
	switch f.Array {
	case ir.ArrayFixed:
		n = f.ArrayBound
	case ir.ArrayCounted:
		n = f.ArrayMin
	case ir.ArrayList:
		n = 0
	}
	if isScalarZero(value) {
		return fmt.Sprintf("[%s] * %d", value, n)
	}
	return fmt.Sprintf("[%s for _s_i in range(%d)]", value, n)
}

func isScalarZero(s string) bool {
	return s == "0" || s == "False" || s == "0.0" || s == "\"\"" || s == "b\"\""
}

func quotedList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = strconv.Quote(n)
	}
	return strings.Join(parts, ", ")
}

func (m *pyModule) emitEquality(name string, fields []*ir.Field) {
	m.line("    def __eq__(self, other: object) -> bool:")
	m.line("        if not isinstance(other, %s):", name)
	m.line("            return NotImplemented")
	if len(fields) == 0 {
		m.line("        return True")
		m.line("")
		return
	}
	var parts []string
	for _, f := range fields {
		parts = append(parts, m.equalExpr("self."+f.Name, "other."+f.Name, f))
	}
	m.line("        return %s", strings.Join(parts, " and "))
	m.line("")
}

// equalExpr compares one field by value, a float by its bits and an array
// element by element so a float element is compared by bits too.
func (m *pyModule) equalExpr(a, b string, f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		elem := *f
		elem.Array = ir.ArrayNone
		inner := m.equalExpr("_s_x", "_s_y", &elem)
		if inner == "_s_x == _s_y" {
			return a + " == " + b
		}
		return fmt.Sprintf("len(%s) == len(%s) and all(%s for _s_x, _s_y in zip(%s, %s))", a, b, inner, a, b)
	}
	switch f.Type.Kind {
	case ir.TFloat32:
		m.needSchema("float32_bits_eq")
		return fmt.Sprintf("float32_bits_eq(%s, %s)", a, b)
	case ir.TFloat64:
		m.needSchema("float64_bits_eq")
		return fmt.Sprintf("float64_bits_eq(%s, %s)", a, b)
	}
	return a + " == " + b
}

func (m *pyModule) emitRepr(name string, fields []*ir.Field) {
	m.line("    def __repr__(self) -> str:")
	if len(fields) == 0 {
		m.line("        return %q", name+"()")
		m.line("")
		return
	}
	var parts []string
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%s={self.%s!r}", f.Name, f.Name))
	}
	m.line("        return f\"%s(%s)\"", name, strings.Join(parts, ", "))
	m.line("")
}

func (m *pyModule) emitUnionEquality(d *ir.Union, arms []ir.UnionVariant) {
	m.line("    def __eq__(self, other: object) -> bool:")
	m.line("        if not isinstance(other, %s):", d.Name)
	m.line("            return NotImplemented")
	m.line("        if self.type != other.type:")
	m.line("            return False")
	for i, v := range arms {
		cond := "if"
		if i > 0 {
			cond = "elif"
		}
		m.line("        %s self.type == %d:", cond, i+1)
		m.line("            return %s", m.equalExpr("self."+v.Name, "other."+v.Name, v.F))
	}
	m.line("        return True")
	m.line("")
}

func (m *pyModule) emitUnionRepr(d *ir.Union, arms []ir.UnionVariant) {
	m.line("    def __repr__(self) -> str:")
	parts := []string{"type={self.type!r}"}
	for _, v := range arms {
		parts = append(parts, fmt.Sprintf("%s={self.%s!r}", v.Name, v.Name))
	}
	m.line("        return f\"%s(%s)\"", d.Name, strings.Join(parts, ", "))
	m.line("")
}

func docComment(doc, indent, marker string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(doc, "\n"), "\n") {
		fmt.Fprintf(&b, "%s%s %s\n", indent, marker, line)
	}
	return b.String()
}
