package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitFixedValues connects the fixed reader's records to the same values the
// text and cook writers use. Only the live extent crosses the boundary: an
// absent optional has no value, and a counted array has exactly count elements
// (docs/SPEC-TABLES.md §3.4, §7.6, §16).
func (g *gen) emitFixedValues(b *strings.Builder, names []string) {
	types, unions := unitFixedClosure(g.unit, g.closure)
	for _, name := range names {
		if !types[name] || !g.rowable(name) {
			continue
		}
		st := vStruct(g.unit, name)
		fmt.Fprintf(b, "impl TryFrom<&crate::%sRow> for %s {\n    type Error = ();\n", name, name)
		b.WriteString("    #[allow(unused_variables)]\n")
		fmt.Fprintf(b, "    fn try_from(row: &crate::%sRow) -> Result<Self, ()> {\n        Ok(Self {\n", name)
		for _, f := range st.Fields {
			fmt.Fprintf(b, "            %s: %s,\n", ir.RustSnake(f.Name), fixedValueExpr(f, "row."+f.Name))
		}
		b.WriteString("        })\n    }\n}\n\n")
		fmt.Fprintf(b, "impl From<&%s> for crate::%sRow {\n", name, name)
		b.WriteString("    #[allow(unused_mut, unused_variables)]\n")
		fmt.Fprintf(b, "    fn from(value: &%s) -> Self {\n        let mut row = Self::default();\n", name)
		for _, f := range st.Fields {
			emitFixedValueStore(b, f, "row."+f.Name, "value."+ir.RustSnake(f.Name), "        ")
		}
		b.WriteString("        row\n    }\n}\n\n")
	}
	for _, un := range g.collectVariableUnions(names) {
		if !unions[un.Name] || !unionRowable(un) {
			continue
		}
		fmt.Fprintf(b, "impl TryFrom<&crate::%sRow> for %s {\n    type Error = ();\n", un.Name, un.Name)
		fmt.Fprintf(b, "    fn try_from(row: &crate::%sRow) -> Result<Self, ()> {\n        Ok(match row.tag() {\n            0 => Self::None,\n", un.Name)
		for i, v := range un.Variants {
			if v.Void() {
				fmt.Fprintf(b, "            %d => Self::%s,\n", i+1, rustVariant(v.Name))
				continue
			}
			fmt.Fprintf(b, "            %d => {\n                let arm = row.%s().ok_or(())?;\n", i+1, v.Name)
			x := "arm"
			if _, companion := unionArmCompanionType(un, v); companion {
				x = "arm.value"
			}
			fmt.Fprintf(b, "                Self::%s(%s)\n            }\n", rustVariant(v.Name), fixedValueExpr(v.F, x))
		}
		b.WriteString("            _ => return Err(()),\n        })\n    }\n}\n\n")
		fmt.Fprintf(b, "impl From<&%s> for crate::%sRow {\n", un.Name, un.Name)
		fmt.Fprintf(b, "    fn from(value: &%s) -> Self {\n        let mut row = Self::default();\n        match value {\n            %s::None => {}\n", un.Name, un.Name)
		for _, v := range un.Variants {
			if v.Void() {
				fmt.Fprintf(b, "            %s::%s => row.set_%s(),\n", un.Name, rustVariant(v.Name), v.Name)
				continue
			}
			fmt.Fprintf(b, "            %s::%s(value) => {\n", un.Name, rustVariant(v.Name))
			if companion, ok := unionArmCompanionType(un, v); ok {
				fmt.Fprintf(b, "                let mut arm = crate::%s::default();\n", companion)
				emitFixedValueStore(b, v.F, "arm.value", "(*value)", "                ")
				fmt.Fprintf(b, "                row.set_%s(arm);\n", v.Name)
			} else if v.F.Array != ir.ArrayNone {
				fmt.Fprintf(b, "                let mut arm = [Default::default(); %d];\n", v.F.ArrayBound)
				emitFixedValueStore(b, v.F, "arm", "(*value)", "                ")
				fmt.Fprintf(b, "                row.set_%s(arm);\n", v.Name)
			} else {
				fmt.Fprintf(b, "                row.set_%s(%s);\n", v.Name, fixedScalarExpr(v.F, "(*value)", true))
			}
			b.WriteString("            }\n")
		}
		b.WriteString("        }\n        row\n    }\n}\n\n")
	}
}

func fixedValueExpr(f *ir.Field, x string) string {
	if f.Type.Optional {
		e := *f
		e.Type.Optional = false
		return fmt.Sprintf("if %s_present { Some(%s) } else { None }", x, fixedValueExpr(&e, x))
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		n := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
			n = x + "_count as usize"
		}
		e := *f
		e.Array, e.KeyEnum = ir.ArrayNone, ""
		return fmt.Sprintf("%s.get(..%s).ok_or(())?.iter().map(|element| Ok(%s)).collect::<Result<Vec<_>, ()>>()?", x, n, fixedScalarExpr(&e, "(*element)", false))
	}
	if f.Type.Kind == ir.TString {
		return fmt.Sprintf("String::from_utf8(%s.get(..%s_length as usize).ok_or(())?.to_vec()).map_err(|_| ())?", x, x)
	}
	if f.Type.Kind == ir.TBytes {
		return fmt.Sprintf("%s.get(..%s_length as usize).ok_or(())?.to_vec()", x, x)
	}
	return fixedScalarExpr(f, x, false)
}

func fixedScalarExpr(f *ir.Field, x string, toRow bool) string {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			n := f.Type.Name
			if toRow {
				n = "crate::" + n + "Row"
				return n + "::from(" + borrowOf(x) + ")"
			}
			return n + "::try_from(" + borrowOf(x) + ")?"
		case *ir.Enum:
			var arms []string
			for i, v := range ref.Variants {
				variant := ref.Name + "::" + rustVariant(v)
				if toRow {
					arms = append(arms, fmt.Sprintf("%s => %d", variant, i+1))
				} else {
					arms = append(arms, fmt.Sprintf("%d => %s", i+1, variant))
				}
			}
			fallback := "return Err(())"
			if toRow {
				fallback = "0"
			} else {
				arms = append(arms, "0 => "+ref.Name+"::None")
			}
			arms = append(arms, "_ => "+fallback)
			return "match " + unparen(x) + " { " + strings.Join(arms, ", ") + " }"
		}
	}
	if (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width > 64 {
		if toRow {
			return "crate::" + cookElemType(f) + "(" + x + ")"
		}
		return x + ".0"
	}
	if have, want := cookElemType(f), scalarRust(f); have != want {
		if toRow {
			want = have
			return unparen(x) + " as " + want
		}
		return want + "::try_from(" + unparen(x) + ").map_err(|_| ())?"
	}
	return unparen(x)
}

func emitFixedValueStore(b *strings.Builder, f *ir.Field, dst, src, ind string) {
	if f.Type.Optional {
		e := *f
		e.Type.Optional = false
		fmt.Fprintf(b, "%sif let Some(present) = &%s {\n%s    %s_present = true;\n", ind, src, ind, dst)
		emitFixedValueStore(b, &e, dst, "(*present)", ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		fmt.Fprintf(b, "%sdebug_assert!(%s.len() <= %d);\n", ind, src, f.ArrayBound)
		if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
			fmt.Fprintf(b, "%s%s_count = %s.len() as i32;\n", ind, dst, src)
		}
		e := *f
		e.Array, e.KeyEnum = ir.ArrayNone, ""
		fmt.Fprintf(b, "%sfor (slot, element) in %s.iter_mut().zip(%s.iter()) {\n", ind, dst, src)
		fmt.Fprintf(b, "%s    *slot = %s;\n%s}\n", ind, fixedScalarExpr(&e, "(*element)", true), ind)
		return
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		bytes := src
		if f.Type.Kind == ir.TString {
			bytes += ".as_bytes()"
		} else {
			bytes = borrowOf(src)
		}
		fmt.Fprintf(b, "%sdebug_assert!(%s.len() <= %d);\n", ind, src, f.Type.Size)
		fmt.Fprintf(b, "%s%s_length = %s.len() as i32;\n", ind, dst, src)
		fmt.Fprintf(b, "%s%s[..%s.len()].copy_from_slice(%s);\n", ind, dst, src, bytes)
		return
	}
	fmt.Fprintf(b, "%s%s = %s;\n", ind, dst, fixedScalarExpr(f, src, true))
}
