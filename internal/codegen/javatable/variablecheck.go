package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VARIABLE FORM'S WRITE CONTRACT IS A DEBUG ASSERT (docs/SPEC.md §5.1):
// Java is an assert leg, so the variable-form save checks rows 1-5 and 8 —
// a ranged or kind-bounded integer, a bits(N) width, a counted array's count,
// a string(N) or bytes(N) length, an enum ordinal, and well-formed text — in
// one checkWrite predicate per type, reached once through `assert` from save.
// Without -ea the predicate never runs and the writer trusts its caller.
// Row 10 (a ranged float off its range) is not here: the Java row holds it in
// every build, on the writer's own float put (putF32In, putF64In).

// writeVariableCheckWrite emits a table's checkWrite: every field the save
// writes, under the same guard the save writes it under. The one-argument
// form is the root's, asserted from save before the graph walk: it walks
// every table the value reaches, through its pointers as well, each once
// (TableVariable.WriteCheck keeps the seen set), so a cycle ends the walk
// and stays the graph's own refusal. The two-argument form checks one
// table's fields and hands each pointer's target to the walk.
func writeVariableCheckWrite(b *strings.Builder, st *ir.Struct, tables []*ir.Struct) {
	b.WriteString("    // checkWrite is save's contract walk (docs/SPEC.md §5.1 rows 1-5 and 8),\n")
	b.WriteString("    // called through `assert`: THE WRITE-SIDE CHECKS ARE DEBUG ONLY, and\n")
	b.WriteString("    // without -ea the writer trusts its caller. It reaches every table the\n")
	b.WriteString("    // value points to, each once, so a cycle ends it.\n")
	b.WriteString("    static boolean checkWrite(Value v) {\n")
	b.WriteString("        TableVariable.WriteCheck walk = new TableVariable.WriteCheck();\n")
	b.WriteString("        walk.point(v);\n")
	b.WriteString("        for (Object o = walk.next(); o != null; o = walk.next()) {\n")
	for _, t := range tables {
		fmt.Fprintf(b, "            if (o instanceof %sVariable.Value) {\n", t.Name)
		fmt.Fprintf(b, "                assert %sVariable.checkWrite((%sVariable.Value) o, walk);\n", t.Name, t.Name)
		b.WriteString("            }\n")
	}
	b.WriteString("        }\n")
	b.WriteString("        return true;\n    }\n\n")
	b.WriteString("    static boolean checkWrite(Value v, TableVariable.WriteCheck walk) {\n")
	b.WriteString("        if (v == null) { return true; }\n")
	for _, f := range st.Fields {
		var fb strings.Builder
		writeVariableFieldCheck(&fb, f, "v."+varMember(f), st.DeclaredName()+"."+f.Name, "        ")
		if fb.Len() == 0 {
			continue
		}
		if cond := variableGuard(st, f); cond != "" {
			b.WriteString("        if (" + cond + ") {\n")
			b.WriteString(shiftLines(fb.String(), 4))
			b.WriteString("        }\n")
			continue
		}
		b.WriteString(fb.String())
	}
	b.WriteString("        return true;\n    }\n\n")
}

// writeVariableUnionCheckWrite emits a union's checkWrite: the selected arm's
// payload only. A tag outside the arm set is the save's own refusal (row 6).
func writeVariableUnionCheckWrite(b *strings.Builder, un *ir.Union) {
	mem := unionMembers(un)
	b.WriteString("    // checkWrite is the selected arm's contract walk (docs/SPEC.md §5.1),\n")
	b.WriteString("    // reached from its holder's checkWrite under `assert`: debug only.\n")
	b.WriteString("    static boolean checkWrite(Value v, TableVariable.WriteCheck walk) {\n")
	b.WriteString("        if (v == null) { return true; }\n")
	var cases strings.Builder
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		one := *arm.F
		one.Name = mem[i]
		for _, f := range variableStruct(&ir.Struct{Name: un.Name, Fields: []*ir.Field{&one}}).Fields {
			var fb strings.Builder
			writeVariableFieldCheck(&fb, f, "v."+varMember(f), un.Name+"."+arm.Name, "            ")
			if fb.Len() == 0 {
				continue
			}
			fmt.Fprintf(&cases, "        if (v.tag == %d) {\n", i+1)
			cases.WriteString(fb.String())
			cases.WriteString("        }\n")
		}
	}
	b.WriteString(cases.String())
	b.WriteString("        return true;\n    }\n\n")
}

// writeVariableFieldCheck emits one field's asserts: presence first, then the
// count, then each element (or the one value) by its kind.
func writeVariableFieldCheck(b *strings.Builder, f *ir.Field, expr, name, ind string) {
	if f.Type.Optional {
		var inner strings.Builder
		one := *f
		one.Type.Optional = false
		writeVariableFieldCheck(&inner, &one, expr, name, ind+"    ")
		if inner.Len() > 0 {
			fmt.Fprintf(b, "%sif (%sPresent) {\n%s%s}\n", ind, expr, inner.String(), ind)
		}
		return
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		if f.Array == ir.ArrayCounted {
			// ROW 3: a counted array's count inside [Min, Max]
			if f.ArrayMin > 0 {
				fmt.Fprintf(b, "%sassert %s.size() >= %d && %s.size() <= %d : %q;\n", ind, expr, f.ArrayMin, expr, f.ArrayBound, name)
			} else {
				fmt.Fprintf(b, "%sassert %s.size() <= %d : %q;\n", ind, expr, f.ArrayBound, name)
			}
		}
		one := variableElement(f)
		var inner strings.Builder
		writeVariableValueCheck(&inner, one, "e", name, ind+"    ")
		if inner.Len() == 0 {
			return
		}
		count := expr + ".size()"
		if f.Array == ir.ArrayFixed {
			// a fixed array writes its first N slots and no others
			count = fmt.Sprintf("Math.min(%s.size(), %d)", expr, f.ArrayBound)
		}
		fmt.Fprintf(b, "%sfor (int i = 0, n = %s; i < n; i++) {\n", ind, count)
		fmt.Fprintf(b, "%s    %s e = %s.get(i);\n", ind, variableValueType(one), expr)
		b.WriteString(inner.String())
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	writeVariableValueCheck(b, f, expr, name, ind)
}

// writeVariableValueCheck emits the asserts for one value of f's kind.
func writeVariableValueCheck(b *strings.Builder, f *ir.Field, expr, name, ind string) {
	if f.Type.Pointer && isStruct(f) {
		// a pointer's target is a table of its own: the walk checks it once
		fmt.Fprintf(b, "%swalk.point(%s);\n", ind, expr)
		return
	}
	if variableWide(f) {
		// ROW 1: a fixed-point raw value or a wide integer off its range
		_, _, ranged := ir.TableRawRange(f)
		if !ranged && (f.Type.Width == 32 || f.Type.Width == 64 || f.Type.Width == 128) {
			return
		}
		lo, hi := variableRawBounds(f)
		raw := variableRawExpr(f, expr)
		fmt.Fprintf(b, "%sassert %s.compareTo(%s) >= 0 && %s.compareTo(%s) <= 0 : %q;\n", ind, raw, lo, raw, hi, name)
		return
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
		// ROWS 1 AND 2: the declared range, else the kind's own, against the
		// value the Java storage holds (an int8 lives in an int)
		if !f.HasIntRange && (f.Type.Width == 32 || f.Type.Width == 64) {
			return // the storage holds exactly the kind's values
		}
		min, max := jsonIntRange(f)
		var lower, upper string
		switch {
		case f.Type.Signed && f.Type.Kind == ir.TInt:
			lower, upper = fmt.Sprintf("%s >= %s", expr, min), fmt.Sprintf("%s <= %s", expr, max)
		case f.Type.Width <= 32:
			// an unsigned kind of 32 bits or fewer lives in an int: its value
			// is the int's low 32 bits, never its sign
			lower, upper = fmt.Sprintf("(%s & 0xffffffffL) >= %s", expr, min), fmt.Sprintf("(%s & 0xffffffffL) <= %s", expr, max)
		default:
			lower, upper = fmt.Sprintf("Long.compareUnsigned(%s, %s) >= 0", expr, min), fmt.Sprintf("Long.compareUnsigned(%s, %s) <= 0", expr, max)
		}
		if min == "0L" {
			fmt.Fprintf(b, "%sassert %s : %q;\n", ind, upper, name)
		} else {
			fmt.Fprintf(b, "%sassert %s && %s : %q;\n", ind, lower, upper, name)
		}
	case ir.TString:
		// ROWS 4 AND 8: UTF-8 length inside N, no lone surrogate, no zero
		bound := int64(-1)
		if f.Type.Size > 0 && !f.Type.TextUnbounded() {
			bound = int64(f.Type.Size)
		}
		fmt.Fprintf(b, "%sassert TableVariable.textWritable(%s, %d) : %q;\n", ind, expr, bound, name)
	case ir.TBytes:
		// ROW 4: a bytes(N) length inside N
		if f.Type.Size > 0 {
			fmt.Fprintf(b, "%sassert %s == null || %s.length <= %d : %q;\n", ind, expr, expr, f.Type.Size, name)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// ROW 5: an ordinal inside the variant set (0 is the unset value)
			fmt.Fprintf(b, "%sassert %s >= 0 && %s <= %d : %q;\n", ind, expr, expr, len(ref.Variants), name)
		case *ir.Struct, *ir.Union:
			fmt.Fprintf(b, "%sassert %sVariable.checkWrite(%s, walk);\n", ind, f.Type.Name, expr)
		}
	}
}
