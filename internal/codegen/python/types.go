package python

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// gen is one module being emitted: its body, the module-level constants the
// codecs precompute (compressed float declarations), and the sibling modules
// it references.
type gen struct {
	u        *ir.Unit
	f        *ir.File
	b        strings.Builder
	consts   []string
	siblings map[string]bool
}

func (g *gen) pf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

// ref is a declaration's name as this module spells it: bare when it is
// declared here, else through the sibling module's _s_ binding.
func (g *gen) ref(name string) string {
	base := g.u.DeclFile[name]
	if base == "" || base == g.f.Base {
		return name
	}
	g.siblings[base] = true
	return "_s_" + base + "." + name
}

// fn is a module-level codec function of a declaration, spelled the same way.
func (g *gen) fn(prefix, name string) string {
	base := g.u.DeclFile[name]
	if base == "" || base == g.f.Base {
		return prefix + name
	}
	g.siblings[base] = true
	return "_s_" + base + "." + prefix + name
}

// docstring renders a `///` block as a Python docstring at indent, or
// nothing when there is none.
func docstring(doc, indent string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	doc = strings.ReplaceAll(doc, `\`, `\\`)
	doc = strings.ReplaceAll(doc, `"""`, `\"\"\"`)
	lines := strings.Split(doc, "\n")
	if len(lines) == 1 {
		return indent + `"""` + lines[0] + `"""` + "\n"
	}
	var b strings.Builder
	b.WriteString(indent + `"""` + lines[0] + "\n")
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + l + "\n")
	}
	b.WriteString(indent + `"""` + "\n")
	return b.String()
}

// comment renders a `///` block as # lines at indent.
func comment(doc, indent string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.Split(doc, "\n") {
		b.WriteString(strings.TrimRight(indent+"# "+l, " ") + "\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// literals

func pyInt(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

// pyFloat is a float literal Python reads back to the same double.
func pyFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return `float("inf")`
	case math.IsInf(v, -1):
		return `float("-inf")`
	case math.IsNaN(v):
		return `float("nan")`
	}
	if v == 0 && math.Signbit(v) {
		return "-0.0"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

// pyFloat32 is the literal of v rounded to float32, as Go's float32(v) does.
func pyFloat32(v float64) string { return pyFloat(float64(float32(v))) }

// pyStr is a str literal of UTF-8 text.
func pyStr(b []byte) string { return strconv.Quote(string(b)) }

// pyBytes is a bytes literal, every byte outside printable ASCII escaped.
func pyBytes(b []byte) string {
	var s strings.Builder
	s.WriteString(`b"`)
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			s.WriteByte(c)
		default:
			fmt.Fprintf(&s, `\x%02x`, c)
		}
	}
	s.WriteByte('"')
	return s.String()
}

// ---------------------------------------------------------------------------
// constants, enums, flags

func (g *gen) emitConst(d *ir.Const) {
	g.pf("%s", comment(d.Doc, ""))
	var lit string
	switch {
	case d.IsFloat && d.Storage == "float32":
		lit = pyFloat32(d.Float)
	case d.IsFloat:
		lit = pyFloat(d.Float)
	default:
		lit = pyInt(d.Int)
	}
	// the schema expression rides as a comment where it is not the literal
	note := ""
	if d.Expr != nil {
		if r := ir.RenderExpr(d.Expr); r != lit {
			note = "  # = " + r
		}
	}
	g.pf("%s: _s_t.Final = %s%s\n\n\n", d.Name, lit, note)
}

// emitTags emits an enum's or a union tag's constant class and its namer:
// NONE = 0, each member from 1 as the schema spells it, MAX the wire's top
// value (SPEC §4.2), and EnumName<Name>(value), "???" for an unnamed value.
func (g *gen) emitTags(name, doc, what string, members, docs []string, max int64) {
	g.pf("class %s:\n", name)
	if d := docstring(doc, "    "); d != "" {
		g.pf("%s\n", d)
	} else {
		g.pf("    \"\"\"%s: NONE = 0, then each member from 1; the wire range is [0, MAX].\"\"\"\n\n", what)
	}
	g.pf("    NONE: _s_t.Final = 0\n")
	for i, m := range members {
		g.pf("%s", comment(docs[i], "    "))
		g.pf("    %s: _s_t.Final = %d\n", m, i+1)
	}
	g.pf("    MAX: _s_t.Final = %d\n\n\n", max)
	g.pf("_s_names_%s: _s_t.Final = (\"None\"", name)
	for _, m := range members {
		g.pf(", %q", m)
	}
	if len(members) == 0 {
		g.pf(",")
	}
	g.pf(")\n\n\n")
	g.pf("def EnumName%s(value: int) -> str:\n", name)
	g.pf("    \"\"\"The name of a %s value, \"???\" for a value no member names.\"\"\"\n", name)
	g.pf("    return _s_names_%s[value] if 0 <= value < %d else \"???\"\n\n\n", name, len(members)+1)
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.emitTags(d.Name, d.Doc, "enum "+d.Name, d.Variants, d.VariantDocs, d.Max)
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.pf("class %s:\n", d.Name)
	if doc := docstring(d.Doc, "    "); doc != "" {
		g.pf("%s\n", doc)
	} else {
		g.pf("    \"\"\"flags %s: bit i is member i; the value is the uint64 mask, %d bits on the wire.\"\"\"\n\n", d.Name, d.WireBits)
	}
	for i, v := range d.Variants {
		g.pf("%s", comment(d.VariantDocs[i], "    "))
		g.pf("    %s: _s_t.Final = 1 << %d\n", v, i)
	}
	g.pf("    COUNT: _s_t.Final = %d\n\n\n", len(d.Variants))
	g.pf("_s_bits_%s: _s_t.Final = (", d.Name)
	for i, v := range d.Variants {
		if i > 0 {
			g.pf(", ")
		}
		g.pf("%q", v)
	}
	if len(d.Variants) == 1 {
		g.pf(",")
	}
	g.pf(")\n\n\n")
	g.pf("def FlagName%s(bit: int) -> str:\n", d.Name)
	g.pf("    \"\"\"The name of bit i of %s, \"???\" past the declared members.\"\"\"\n", d.Name)
	g.pf("    return _s_bits_%s[bit] if 0 <= bit < %d else \"???\"\n\n\n", d.Name, len(d.Variants))
}

// ---------------------------------------------------------------------------
// types and unions

// pyType is a field's Python annotation (docs/PORT-PYTHON.md, THE VALUE
// MAPPING). Annotations are only ever written inside a method, where a
// field named like a builtin cannot shadow it.
func (g *gen) pyType(f *ir.Field) string {
	elem := g.pyElemType(f.Type)
	if f.Array != ir.ArrayNone {
		return "list[" + elem + "]"
	}
	return elem
}

func (g *gen) pyElemType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TBits, ir.TFixed:
		return "int"
	case ir.TFloat32, ir.TFloat64:
		return "float"
	case ir.TString, ir.TWString:
		return "str"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "int"
		}
		return g.ref(t.Name)
	}
	return "object"
}

// defaultExpr is a field's value at construction (SPEC §5): the declared
// default, else the type's own; a counted array born at its minimum count
// (SPEC §4.6).
func (g *gen) defaultExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		n := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			n = f.BornCount()
		}
		return g.listOf(n, g.scalarDefault(f), f.Type)
	}
	return g.scalarDefault(f)
}

func (g *gen) zeroExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		if f.Array == ir.ArrayCounted {
			return "[]"
		}
		return g.listOf(f.ArrayBound, g.scalarZero(f.Type), f.Type)
	}
	return g.scalarZero(f.Type)
}

// listOf is a list of n elements, each its own object when the element is a
// generated value (never one object shared by two slots).
func (g *gen) listOf(n int64, elem string, t ir.FieldType) string {
	if n == 0 {
		return "[]"
	}
	if isComposite(t) {
		return fmt.Sprintf("[%s for _ in range(%d)]", elem, n)
	}
	return fmt.Sprintf("[%s] * %d", elem, n)
}

func isComposite(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

func (g *gen) scalarDefault(f *ir.Field) string {
	t := f.Type
	if !f.HasDefault {
		if t.Kind == ir.TNamed && isComposite(t) {
			return g.ref(t.Name) + "()"
		}
		return g.scalarZero(t)
	}
	switch t.Kind {
	case ir.TBool:
		if f.DefBool {
			return "True"
		}
		return "False"
	case ir.TInt, ir.TBits, ir.TFixed:
		return pyInt(f.DefInt)
	case ir.TFloat32:
		return pyFloat32(f.DefFloat)
	case ir.TFloat64:
		return pyFloat(f.DefFloat)
	case ir.TString:
		return pyStr(f.DefBytes)
	case ir.TBytes:
		return pyBytes(f.DefBytes)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			for i, v := range ref.Variants {
				if v == f.DefVariant {
					return fmt.Sprintf("%d", i+1)
				}
			}
			return "0"
		case *ir.Flags:
			return pyInt(f.DefInt)
		}
	}
	return g.scalarZero(t)
}

func (g *gen) scalarZero(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "False"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TString, ir.TWString:
		return `""`
	case ir.TBytes:
		return `b""`
	case ir.TNamed:
		if isComposite(t) {
			return g.ref(t.Name) + "._s_zeroed()"
		}
	}
	return "0"
}

// emitStruct emits a type's class: __slots__, the constructor at the declared
// defaults, the §5 zero form, equality and repr through the runtime, and the
// packet API (read and write), then its codec functions.
func (g *gen) emitStruct(d *ir.Struct) error {
	g.pf("class %s:\n", d.Name)
	if doc := docstring(d.Doc, "    "); doc != "" {
		g.pf("%s\n", doc)
	} else {
		g.pf("    \"\"\"type %s.\"\"\"\n\n", d.Name)
	}
	g.pf("    __slots__ = (")
	for i, f := range d.Fields {
		if i > 0 {
			g.pf(", ")
		}
		g.pf("%q", f.Name)
	}
	if len(d.Fields) == 1 {
		g.pf(",")
	}
	g.pf(")\n\n")
	maxBits := ir.MaxBitsStruct(d)
	g.pf("    MAX_BITS: _s_t.Final = %d\n", maxBits)
	g.pf("    MAX_BYTES: _s_t.Final = %d\n\n", ir.MaxBytes(maxBits))
	g.pf("    def __init__(self) -> None:\n")
	if len(d.Fields) == 0 {
		g.pf("        pass\n")
	}
	for _, f := range d.Fields {
		g.pf("%s", comment(f.Doc, "        "))
		g.pf("        self.%s: %s = %s\n", f.Name, g.pyType(f), g.defaultExpr(f))
	}
	g.pf("\n    @classmethod\n")
	g.pf("    def _s_zeroed(cls) -> %s:\n", d.Name)
	g.pf("        v = cls.__new__(cls)\n")
	for _, f := range d.Fields {
		g.pf("        v.%s = %s\n", f.Name, g.zeroExpr(f))
	}
	g.pf("        return v\n\n")
	// a capacity that is a whole number of 64-bit words
	capacity := (ir.MaxBytes(maxBits) + 7) / 8 * 8
	if capacity == 0 {
		capacity = 8
	}
	g.pf("    def write(self) -> bytes:\n")
	g.pf("        \"\"\"The type wire (SPEC §4.3), or InvalidValue naming the field and the rule.\"\"\"\n")
	g.pf("        w = _s_ser.WriteStream(%d)\n", capacity)
	g.pf("        try:\n")
	g.pf("            _s_w_%s(w, self, _s_ser.Ref(None))\n", d.Name)
	g.pf("        except _s_rt.InvalidValue as e:\n")
	g.pf("            raise e.within(%q) from None\n", d.Name)
	g.pf("        w.flush()\n")
	g.pf("        return w.data()\n\n")
	g.pf("    @classmethod\n")
	g.pf("    def read(cls, data: bytes | bytearray | memoryview) -> %s:\n", d.Name)
	g.pf("        \"\"\"The value the type wire holds, read whole, or Refused with no value.\"\"\"\n")
	g.pf("        r = _s_ser.ReadStream(_s_rt.packet_input(data))\n")
	g.pf("        v = cls()\n")
	g.pf("        if _s_r_%s(r, v, _s_ser.Ref(None)) and r.serialize_end():\n", d.Name)
	g.pf("            return v\n")
	g.pf("        raise _s_rt.Refused(r.refusal() or _s_ser.Refusal.VALUE_OUT_OF_RANGE)\n\n")
	g.pf("    def __eq__(self, other: object) -> bool:\n")
	g.pf("        return _s_rt.equal(self, other)\n\n")
	g.pf("    def __repr__(self) -> str:\n")
	g.pf("        return _s_rt.describe(self)\n\n\n")
	if err := g.emitWriteStruct(d); err != nil {
		return err
	}
	return g.emitReadStruct(d)
}

// unionArms is a union's payload arms: (variant index from 1, the arm as a
// field named for the arm).
func unionArms(d *ir.Union) []unionArm {
	var arms []unionArm
	for i, v := range d.Variants {
		if v.Void() {
			continue
		}
		f := *v.F
		f.Name = v.Name
		arms = append(arms, unionArm{tag: i + 1, f: &f})
	}
	return arms
}

type unionArm struct {
	tag int
	f   *ir.Field
}

func (g *gen) emitUnion(d *ir.Union) error {
	members := make([]string, len(d.Variants))
	docs := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
		docs[i] = v.Doc
	}
	g.emitTags(d.Name+"Type", "", "union "+d.Name+"'s tag", members, docs, d.Max)
	arms := unionArms(d)
	g.pf("class %s:\n", d.Name)
	if doc := docstring(d.Doc, "    "); doc != "" {
		g.pf("%s\n", doc)
	} else {
		g.pf("    \"\"\"union %s: type is the arm (%sType), every payload arm built.\"\"\"\n\n", d.Name, d.Name)
	}
	g.pf("    __slots__ = (\"type\"")
	for _, a := range arms {
		g.pf(", %q", a.f.Name)
	}
	g.pf(")\n\n")
	g.pf("    def __init__(self) -> None:\n")
	g.pf("        self.type: int = 0\n")
	for _, a := range arms {
		g.pf("        self.%s: %s = %s\n", a.f.Name, g.pyType(a.f), g.defaultExpr(a.f))
	}
	g.pf("\n    @classmethod\n")
	g.pf("    def _s_zeroed(cls) -> %s:\n", d.Name)
	g.pf("        v = cls.__new__(cls)\n")
	g.pf("        v.type = 0\n")
	for _, a := range arms {
		g.pf("        v.%s = %s\n", a.f.Name, g.zeroExpr(a.f))
	}
	g.pf("        return v\n\n")
	g.pf("    def __eq__(self, other: object) -> bool:\n")
	g.pf("        return _s_rt.equal(self, other)\n\n")
	g.pf("    def __repr__(self) -> str:\n")
	g.pf("        return _s_rt.describe(self)\n\n\n")
	if err := g.emitWriteUnion(d, arms); err != nil {
		return err
	}
	return g.emitReadUnion(d, arms)
}
