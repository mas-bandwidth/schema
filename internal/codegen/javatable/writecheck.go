package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE CONTRACT OF THE VARIABLE FORM (docs/SPEC.md §5.1): a save refuses
// a value its schema cannot hold. The every-build refusals live in buildGraph
// and the writer (a required ranged integer, a text list past its bounds, a
// wide string past N, a union tag that names no arm, a NaN or an infinity).
// Every other contract is checked in debug: one checkWrite predicate per
// class, called once from buildGraph as `assert checkWrite(v)`, so the
// dormant assert costs the walk one small call, as the packet writer's
// checkWrite<Name> does (internal/codegen/java/functions.go). buildGraph is
// the pre-walk every save (save, saveRetain, toJson) runs over every table
// the value reaches, so a refusal fires before a byte is written, and each
// class checks only its own fields.
//
// The contracts: a ranged integer inside its min/max (scalars and every
// element of an array of them), a bits(N) value inside N bits, a bounded or
// fixed array's count inside its bounds, a string(N) or bytes(N) inside N
// bytes, and an enum ordinal inside its set (0, None, through the last
// variant).

// writeCheckCond is the Java condition under which ONE value of field f (an
// element's field for an array) breaks the write contract, over expr; ""
// when no debug contract binds it. A required scalar's range is left to the
// every-build refusal in buildGraph.
func writeCheckCond(f *ir.Field, expr string) string {
	if variableWide(f) || f.Type.Pointer || f.Type.Blob() {
		return ""
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
		if f.HasIntRange {
			if f.Required && f.Array == ir.ArrayNone && f.KeyEnum == "" {
				return ""
			}
			min, max := jsonIntRange(f)
			switch {
			case f.Type.Signed && f.Type.Kind == ir.TInt && f.Type.Width > 32:
				return fmt.Sprintf("%s < %s || %s > %s", expr, min, expr, max)
			case f.Type.Signed && f.Type.Kind == ir.TInt:
				return fmt.Sprintf("(long) %s < %s || (long) %s > %s", expr, min, expr, max)
			case f.Type.Width <= 32:
				return fmt.Sprintf("Long.compareUnsigned(((long) %s) & 0xffffffffL, %s) < 0 || Long.compareUnsigned(((long) %s) & 0xffffffffL, %s) > 0", expr, min, expr, max)
			default:
				return fmt.Sprintf("Long.compareUnsigned(%s, %s) < 0 || Long.compareUnsigned(%s, %s) > 0", expr, min, expr, max)
			}
		}
		if f.Type.Kind == ir.TBits {
			switch {
			case f.Type.Width < 32:
				return fmt.Sprintf("(%s >>> %d) != 0", expr, f.Type.Width)
			case f.Type.Width > 32 && f.Type.Width < 64:
				return fmt.Sprintf("(%s >>> %d) != 0L", expr, f.Type.Width)
			}
		}
	case ir.TString:
		if f.Type.Size > 0 && !f.Type.TextUnbounded() {
			return fmt.Sprintf("TableVariable.utf8Len(%s) > %dL", expr, f.Type.Size)
		}
	case ir.TBytes:
		if f.Type.Size > 0 && f.Array == ir.ArrayNone {
			return fmt.Sprintf("%s != null && %s.length > %d", expr, expr, f.Type.Size)
		}
	case ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			return fmt.Sprintf("%s < 0 || %s > %d", expr, expr, len(enum.Variants))
		}
	}
	return ""
}

// writeVariableCheckField emits one field's write contract into checkWrite:
// `return false` on the first value that breaks it.
func writeVariableCheckField(b *strings.Builder, f *ir.Field) {
	m := varMember(f)
	var body strings.Builder
	if f.Array != ir.ArrayNone && !f.IsTextList() {
		// a TEXT LIST's count and lengths are refused in every build (buildGraph)
		switch {
		case f.Array == ir.ArrayCounted && f.ArrayMin > 0:
			fmt.Fprintf(&body, "        if (v.%s.size() < %d || v.%s.size() > %d) return false;\n", m, f.ArrayMin, m, f.ArrayBound)
		case f.ArrayBound > 0:
			fmt.Fprintf(&body, "        if (v.%s.size() > %d) return false;\n", m, f.ArrayBound)
		}
	}
	if isScalarArray(f) && variableUnion(f) == nil {
		if cond := writeCheckCond(variableElement(f), "e"); cond != "" {
			fmt.Fprintf(&body, "        for (%s e : v.%s) { if (e != null && (%s)) return false; }\n", variableListElemType(f), m, cond)
		}
	} else if f.Array == ir.ArrayNone {
		if cond := writeCheckCond(f, "v."+m); cond != "" {
			fmt.Fprintf(&body, "        if (%s) return false;\n", cond)
		}
	}
	if body.Len() == 0 {
		return
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "        if (v.%sPresent) {\n", m)
		b.WriteString(body.String())
		b.WriteString("        }\n")
		return
	}
	b.WriteString(body.String())
}

// writeVariableCheck emits a table class's checkWrite predicate and reports
// whether it emitted one: a class with no debug contract carries none.
func writeVariableCheck(b *strings.Builder, st *ir.Struct) bool {
	var body strings.Builder
	for _, f := range st.Fields {
		var one strings.Builder
		writeVariableCheckField(&one, f)
		if one.Len() == 0 {
			continue
		}
		open := openGuard(&body, st, f)
		body.WriteString(one.String())
		closeGuard(&body, open)
	}
	if body.Len() == 0 {
		return false
	}
	b.WriteString("    // checkWrite is the write contract buildGraph asserts (-ea): false when\n")
	b.WriteString("    // a field of v holds a value its schema cannot (docs/SPEC.md §5.1).\n")
	b.WriteString("    static boolean checkWrite(Value v) {\n")
	b.WriteString(body.String())
	b.WriteString("        return true;\n    }\n\n")
	return true
}

// writeVariableUnionCheck is writeVariableCheck for a union: the selected
// arm's payload, by its member.
func writeVariableUnionCheck(b *strings.Builder, un *ir.Union, mem []string) bool {
	var body strings.Builder
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		one := *arm.F
		one.Name = mem[i]
		var arms strings.Builder
		writeVariableCheckField(&arms, &one)
		if arms.Len() == 0 {
			continue
		}
		fmt.Fprintf(&body, "        if (v.tag == %d) {\n", i+1)
		body.WriteString(arms.String())
		body.WriteString("        }\n")
	}
	if body.Len() == 0 {
		return false
	}
	b.WriteString("    // checkWrite is the write contract buildGraph asserts (-ea) on the\n")
	b.WriteString("    // selected arm's payload (docs/SPEC.md §5.1).\n")
	b.WriteString("    static boolean checkWrite(Value v) {\n")
	b.WriteString(body.String())
	b.WriteString("        return true;\n    }\n\n")
	return true
}
