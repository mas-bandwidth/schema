package pythontable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A table unit's module carries the schema file's constants, enums and flags
// sets beside its table classes, spelled as the packet surface spells them
// (internal/codegen/python, surface.go; docs/PORT-PYTHON.md, THE VALUE
// MAPPING): a constant is a module-level Final, an enum a class of int
// constants with None_ = 0, the variants from 1, Max and a NAMES table, and a
// flags set a class of masks with Count and a NAMES table. The package's
// __init__.py re-exports each of them.

// valueDecls is a file's constants, enums and flags sets in emission order.
// Tables and their unions are emitted by the table frame; packet types and
// packet unions are not this surface's.
func valueDecls(f *ir.File) []ir.Decl {
	if f == nil {
		return nil
	}
	var out []ir.Decl
	for _, d := range ir.EmissionOrder(f) {
		switch d.(type) {
		case *ir.Const, *ir.Enum, *ir.Flags:
			out = append(out, d)
		}
	}
	return out
}

// valueDeclName is a constant's, enum's or flags set's declared name.
func valueDeclName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Const:
		return d.Name
	case *ir.Enum:
		return d.Name
	case *ir.Flags:
		return d.Name
	}
	return ""
}

func emitValueDecl(b *strings.Builder, d ir.Decl) {
	switch d := d.(type) {
	case *ir.Const:
		emitConst(b, d)
	case *ir.Enum:
		emitEnum(b, d)
	case *ir.Flags:
		emitFlags(b, d)
	}
}

// emitConst emits a schema constant as a module-level Final (SPEC §4.2), with
// the author's expression in a trailing comment where it was folded.
func emitConst(b *strings.Builder, d *ir.Const) {
	var value string
	switch {
	case d.IsFloat && d.Storage == "float32":
		value = constFloatLiteral(float64(float32(d.Float)))
	case d.IsFloat:
		value = constFloatLiteral(d.Float)
	default:
		value = constIntLiteral(d.Int)
	}
	note := ""
	if d.Expr != nil {
		if r := ir.RenderExpr(d.Expr); r != value {
			note = "  # = " + r
		}
	}
	b.WriteString(declComment(d.Doc, ""))
	fmt.Fprintf(b, "%s: Final = %s%s\n\n\n", d.Name, value, note)
}

// emitEnum emits an enum as a class of int constants: None_ = 0, the variants
// from 1, Max, and the name table (SPEC §4.2). A deprecated variant keeps its
// ordinal and its name (SPEC-TABLES §2.15).
func emitEnum(b *strings.Builder, d *ir.Enum) {
	b.WriteString(declComment(d.Doc, ""))
	fmt.Fprintf(b, "class %s:\n", d.Name)
	b.WriteString("    __slots__ = ()\n")
	b.WriteString("    None_ = 0\n")
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) {
			b.WriteString(declComment(d.VariantDocs[i], "    "))
		}
		fmt.Fprintf(b, "    %s = %d\n", v, i+1)
	}
	fmt.Fprintf(b, "    Max = %d\n", d.Max)
	b.WriteString("    NAMES: Final = {0: \"None\"")
	for i, v := range d.Variants {
		fmt.Fprintf(b, ", %d: %s", i+1, strconv.Quote(v))
	}
	b.WriteString("}\n\n\n")
}

// emitFlags emits a flags set as a class of mask constants, its Count and its
// name table (SPEC §4.2).
func emitFlags(b *strings.Builder, d *ir.Flags) {
	b.WriteString(declComment(d.Doc, ""))
	fmt.Fprintf(b, "class %s:\n", d.Name)
	b.WriteString("    __slots__ = ()\n")
	for i, v := range d.Variants {
		if i < len(d.VariantDocs) {
			b.WriteString(declComment(d.VariantDocs[i], "    "))
		}
		fmt.Fprintf(b, "    %s = 1 << %d\n", v, i)
	}
	fmt.Fprintf(b, "    Count = %d\n", len(d.Variants))
	b.WriteString("    NAMES: Final = {")
	for i, v := range d.Variants {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%d: %s", i, strconv.Quote(v))
	}
	b.WriteString("}\n\n\n")
}

func declComment(doc, indent string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(doc, "\n"), "\n") {
		fmt.Fprintf(&b, "%s# %s\n", indent, line)
	}
	return b.String()
}

func constIntLiteral(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func constFloatLiteral(v float64) string {
	switch {
	case math.IsNaN(v):
		return "float(\"nan\")"
	case math.IsInf(v, 1):
		return "float(\"inf\")"
	case math.IsInf(v, -1):
		return "-float(\"inf\")"
	case v == 0 && math.Signbit(v):
		return "-0.0"
	}
	return pyFloatLiteral(v)
}
