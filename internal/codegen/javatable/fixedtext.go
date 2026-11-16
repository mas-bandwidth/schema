package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedClosure is the set of types the fixed form (docs/SPEC-TABLES.md §3.4)
// generates a <Type>Fixed class for: the fixed roots and every type their
// bodies reach (nested types and unions). The text form of a fixed root (§16)
// reads the value its fixed read gives and carries it into the variable form's
// value, so only these types get a fromFixed.
type fixedClosure struct {
	structs map[string]bool
	unions  map[string]bool
}

func fixedClosureOf(u *ir.Unit) fixedClosure {
	c := fixedClosure{structs: map[string]bool{}, unions: map[string]bool{}}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range fixedRoots(u) {
		fixedCollectTypes(st, seen, &order)
	}
	for _, st := range order {
		c.structs[st.Name] = true
	}
	for _, un := range fixedUnions(order) {
		c.unions[un.Name] = true
	}
	return c
}

// writeFixedToVariable emits <Type>Variable.fromFixed(<Type>Fixed.Value): the
// schema-aware carry of a fixed-form value into the variable-form value of the
// same type — the pair of the conformance driver's reflection VariableCarry,
// done here with the declared signedness and fixed-point scale the reflection
// cannot see. The fixed form stores a narrow integer bit-transparently in the
// same-width SIGNED type (a uint8's 200 rides a byte as -56), while the
// variable form stores the value itself in an int or a long, so the carry
// zero-extends an unsigned kind and lets Java sign-extend a signed one. st is
// the VARIABLE struct (variableStruct applied); the fixed class carries the
// same fields under the same names.
func writeFixedToVariable(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	fmt.Fprintf(b, "    /** carry a %s value into this value (the text form's reader of a\n", fixedClass(st.Name))
	b.WriteString("     *  fixed root's value; docs/SPEC-TABLES.md §16, §3.4). */\n")
	fmt.Fprintf(b, "    public static Value fromFixed(%s.Value fixed) {\n", fixedClass(st.Name))
	b.WriteString("        Value v = new Value();\n")
	for _, f := range st.Fields {
		writeFixedField(b, u, f, varMember(f), javaName(f.Name))
	}
	b.WriteString("        return v;\n    }\n\n")
}

// writeFixedUnionToVariable emits <Union>Variable.fromFixed(<Union>Fixed.Value):
// the same carry for a union, whose fixed form spells the discriminator `type`
// and the variable form `tag`.
func writeFixedUnionToVariable(b *strings.Builder, u *ir.Unit, un *ir.Union) {
	fmt.Fprintf(b, "    /** carry a %s value into this value (the text form's reader of a\n", fixedClass(un.Name))
	b.WriteString("     *  fixed root's value; docs/SPEC-TABLES.md §16, §3.4). */\n")
	fmt.Fprintf(b, "    public static Value fromFixed(%s.Value fixed) {\n", fixedClass(un.Name))
	b.WriteString("        Value v = new Value();\n")
	b.WriteString("        v.tag = fixed.type;\n")
	mem := unionMembers(un)
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		writeFixedField(b, u, arm.F, mem[i], javaName(arm.F.Name))
	}
	b.WriteString("        return v;\n    }\n\n")
}

// writeFixedField emits the conversion of one field: the optional present flag
// beside its payload, then the payload by kind.
func writeFixedField(b *strings.Builder, u *ir.Unit, f *ir.Field, vname, fname string) {
	if f.Type.Optional {
		fmt.Fprintf(b, "        v.%sPresent = fixed.%sPresent;\n", vname, fname)
	}
	// text and bytes: a fixed buffer beside its live length
	if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && f.Array == ir.ArrayNone && f.KeyEnum == "" {
		if f.Type.Kind == ir.TString {
			fmt.Fprintf(b, "        v.%s = new String(fixed.%s, 0, fixed.%sLength, StandardCharsets.UTF_8);\n", vname, fname, fname)
		} else {
			fmt.Fprintf(b, "        v.%s = java.util.Arrays.copyOf(fixed.%s, fixed.%sLength);\n", vname, fname, fname)
		}
		return
	}
	// a nested type or union, by value
	if f.Array == ir.ArrayNone && f.KeyEnum == "" && f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			fmt.Fprintf(b, "        v.%s = %sVariable.fromFixed(fixed.%s);\n", vname, f.Type.Name, fname)
			return
		}
	}
	// an array (bounded, counted or keyed) of scalars or nested values
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		var bound string
		if f.Array == ir.ArrayCounted {
			bound = "fixed." + fname + "Count"
		} else {
			bound = "fixed." + fname + ".length"
		}
		elem := fixedElementExpr(f, "fixed."+fname+"[i]")
		fmt.Fprintf(b, "        for (int i = 0; i < %s; i++) {\n", bound)
		if f.KeyEnum != "" {
			// a keyed array's list is born pre-filled with the bound's defaults
			// (generateTableVariable's Value constructor), so its slots are
			// set, not appended; a bounded or counted array is born empty.
			fmt.Fprintf(b, "            v.%s.set(i, %s);\n", vname, elem)
		} else {
			fmt.Fprintf(b, "            v.%s.add(%s);\n", vname, elem)
		}
		b.WriteString("        }\n")
		return
	}
	// a scalar leaf
	fmt.Fprintf(b, "        v.%s = %s;\n", vname, fixedScalarExpr(f, "fixed."+fname))
}

// fixedElementExpr converts one fixed array element into the variable list's
// element: a nested value recurses, a scalar masks or widens.
func fixedElementExpr(f *ir.Field, src string) string {
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return f.Type.Name + "Variable.fromFixed(" + src + ")"
		}
	}
	return fixedScalarExpr(f, src)
}

// fixedScalarExpr carries a fixed-form scalar leaf (or array element) into the
// variable form's value: the fixed form's same-width signed storage widens by
// Java's own sign extension for a signed kind, and is masked for an unsigned
// kind — an unsigned integer, a bits(N) (never narrower than int), a fixed
// point whose raw scale has its top bit set, or an enum ordinal.
func fixedScalarExpr(f *ir.Field, src string) string {
	switch fixedScalarType(f.Type) {
	case "byte":
		if f.Type.Signed {
			return "(int) " + src
		}
		return "(" + src + " & 0xFF)"
	case "short":
		if f.Type.Signed {
			return "(int) " + src
		}
		return "(" + src + " & 0xFFFF)"
	}
	return src
}
