package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The bridge is a separate module so a consumer compiling only one wire's
// modules does not acquire a dependency on the other representation.
func (g *gen) fixedValuesModule() []byte {
	names := cardVariableTableNames(g)
	var body strings.Builder
	g.emitFixedValues(&body, names)
	if body.Len() == 0 {
		return nil
	}
	types, unions := unitFixedClosure(g.unit, g.closure)
	wanted := map[string]bool{}
	field := func(f *ir.Field) {
		if f == nil {
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			wanted[ref.Name] = true
		case *ir.Enum:
			wanted[ref.Name] = true
		case *ir.Union:
			wanted[ref.Name] = true
		}
	}
	for _, name := range names {
		if types[name] && g.rowable(name) {
			wanted[name] = true
			for _, f := range vStruct(g.unit, name).Fields {
				field(f)
			}
		}
	}
	for _, un := range g.collectVariableUnions(names) {
		if unions[un.Name] && unionRowable(un) {
			wanted[un.Name] = true
			for _, v := range un.Variants {
				field(v.F)
			}
		}
	}
	var out strings.Builder
	out.WriteString(header(g.file.Base, g.unit.Package, "fixed records and text/cook values"))
	for _, file := range g.unit.Files {
		var symbols []string
		for _, name := range fileVariableTableNames(g.unit, file, g.vc) {
			if wanted[name] {
				symbols = append(symbols, name)
			}
		}
		if file == g.file {
			for _, un := range g.collectVariableUnions(names) {
				if wanted[un.Name] {
					symbols = append(symbols, un.Name)
				}
			}
		}
		if len(symbols) != 0 {
			sort.Strings(symbols)
			fmt.Fprintf(&out, "use super::%s_table::{%s};\n", strings.ToLower(file.Base), strings.Join(symbols, ", "))
		}
	}
	var enums []string
	for _, e := range g.collectVariableEnums(names) {
		if wanted[e.Name] {
			enums = append(enums, e.Name)
		}
	}
	if len(enums) != 0 {
		sort.Strings(enums)
		fmt.Fprintf(&out, "use super::%s::{%s};\n", TableEnumsModule, strings.Join(enums, ", "))
	}
	out.WriteByte('\n')
	out.WriteString(body.String())
	return []byte(strings.TrimRight(out.String(), "\n") + "\n")
}

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
		fmt.Fprintf(b, "impl TryFrom<&super::%sRow> for %s {\n    type Error = ();\n", name, name)
		arg := "row"
		if len(st.Fields) == 0 {
			arg = "_row"
		}
		fmt.Fprintf(b, "    fn try_from(%s: &super::%sRow) -> Result<Self, ()> {\n        Ok(Self {\n", arg, name)
		for _, f := range st.Fields {
			fmt.Fprintf(b, "            %s: %s,\n", ir.RustSnake(f.Name), fixedValueExpr(f, "row."+f.Name))
		}
		b.WriteString("        })\n    }\n}\n\n")
		fmt.Fprintf(b, "impl From<&%s> for super::%sRow {\n", name, name)
		if len(st.Fields) == 0 {
			fmt.Fprintf(b, "    fn from(_value: &%s) -> Self {\n        Self::default()\n    }\n}\n\n", name)
			continue
		}
		var direct, compound []*ir.Field
		for _, f := range st.Fields {
			if !f.Type.Optional && f.Array == ir.ArrayNone && f.KeyEnum == "" && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
				direct = append(direct, f)
			} else {
				compound = append(compound, f)
			}
		}
		fmt.Fprintf(b, "    fn from(value: &%s) -> Self {\n", name)
		if len(compound) == 0 {
			b.WriteString("        Self {\n")
		} else if len(direct) == 0 {
			b.WriteString("        let mut row = Self::default();\n")
		} else {
			b.WriteString("        let mut row = Self {\n")
		}
		for _, f := range direct {
			fmt.Fprintf(b, "            %s: %s,\n", f.Name, fixedScalarExpr(f, "value."+ir.RustSnake(f.Name), true))
		}
		if len(compound) == 0 {
			b.WriteString("        }\n    }\n}\n\n")
			continue
		}
		if len(direct) != 0 {
			b.WriteString("            ..Self::default()\n        };\n")
		}
		for _, f := range compound {
			emitFixedValueStore(b, f, "row."+f.Name, "value."+ir.RustSnake(f.Name), "        ")
		}
		b.WriteString("        row\n    }\n}\n\n")
	}
	for _, un := range g.collectVariableUnions(names) {
		if !unions[un.Name] || !unionRowable(un) {
			continue
		}
		fmt.Fprintf(b, "impl TryFrom<&super::%sRow> for %s {\n    type Error = ();\n", un.Name, un.Name)
		fmt.Fprintf(b, "    fn try_from(row: &super::%sRow) -> Result<Self, ()> {\n        Ok(match row.tag() {\n            0 => Self::None,\n", un.Name)
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
		fmt.Fprintf(b, "impl From<&%s> for super::%sRow {\n", un.Name, un.Name)
		mut := "mut "
		if len(un.Variants) == 0 {
			mut = ""
		}
		fmt.Fprintf(b, "    fn from(value: &%s) -> Self {\n        let %srow = Self::default();\n        match value {\n            %s::None => {}\n", un.Name, mut, un.Name)
		for _, v := range un.Variants {
			if v.Void() {
				fmt.Fprintf(b, "            %s::%s => row.set_%s(),\n", un.Name, rustVariant(v.Name), v.Name)
				continue
			}
			fmt.Fprintf(b, "            %s::%s(value) => {\n", un.Name, rustVariant(v.Name))
			if companion, ok := unionArmCompanionType(un, v); ok {
				fmt.Fprintf(b, "                let mut arm = super::%s::default();\n", companion)
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
		element := fixedScalarExpr(&e, "(*element)", false)
		if strings.HasSuffix(element, "?") {
			element = strings.TrimSuffix(element, "?")
		} else {
			element = "Ok(" + element + ")"
		}
		mapper := "|element| " + element
		switch e.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			mapper = e.Type.Name + "::try_from"
		}
		return fmt.Sprintf("%s.get(..%s).ok_or(())?.iter().map(%s).collect::<Result<Vec<_>, ()>>()?", x, n, mapper)
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
				n = "super::" + n + "Row"
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
			return "super::" + cookElemType(f) + "(" + unparen(x) + ")"
		}
		if unparen(x) != x {
			return borrowOf(x) + ".0"
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
