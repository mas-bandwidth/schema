package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE CONTRACT OF THE FIXED AND MESSAGE FORMS (docs/SPEC.md §5.1): a
// save refuses a value its declaration does not admit. Every type's fixed
// class carries checkWrite, a predicate over one value and everything it
// holds by value, false on the first field that breaks the contract: a
// ranged integer off its min/max (scalars and every stored element), a
// bits(N) value wider than N, an enum ordinal outside its set, a union tag
// outside its arms, a counted array's count or a text's length outside its
// bounds, narrow text that is not well-formed UTF-8, and wide text with an
// unpaired surrogate. THE CHECKS ARE DEBUG ONLY: they run through `assert`
// (-ea), so a release build trusts and pays nothing. The fixed form's save
// and the message form's measureMessages and saveMessages assert
// checkWriteBatch over the whole batch before the first byte is stored, so a
// refused batch leaves the caller's buffer as it was. A NaN or an infinity is
// refused in every build by the finite pass, apart from this.

// fixedCheckCond is the Java condition under which ONE stored value of f (an
// element, for an array) breaks the write contract, over expr; "" when no
// contract binds it here.
func fixedCheckCond(f *ir.Field, expr string) string {
	switch f.Type.Kind {
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return "!" + fixedClass(r.Name) + ".checkWrite(" + expr + ")"
		case *ir.Union:
			return "!" + fixedClass(r.Name) + ".checkWrite(" + expr + ")"
		case *ir.Enum:
			n := len(r.Variants)
			if r.StorageBits >= 64 {
				return fmt.Sprintf("Long.compareUnsigned(%s, %dL) > 0", expr, n)
			}
			return fmt.Sprintf("(((long) %s) & %s) > %dL", expr, fixedMaskLit(fixedJavaBits(fixedIntType(r.StorageBits))), n)
		}
	case ir.TInt, ir.TBits:
		if f.Type.Width > 64 {
			return ""
		}
		var conds []string
		storage := fixedJavaBits(fixedScalarType(f.Type))
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			min, max := jsonIntRange(f)
			switch {
			case f.Type.Signed && f.Type.Kind == ir.TInt && storage == 64:
				conds = append(conds, fmt.Sprintf("%s < %s || %s > %s", expr, min, expr, max))
			case f.Type.Signed && f.Type.Kind == ir.TInt:
				conds = append(conds, fmt.Sprintf("(long) %s < %s || (long) %s > %s", expr, min, expr, max))
			case storage < 64:
				u := fmt.Sprintf("(((long) %s) & %s)", expr, fixedMaskLit(storage))
				conds = append(conds, fmt.Sprintf("%s < %s || %s > %s", u, min, u, max))
			default:
				conds = append(conds, fmt.Sprintf("Long.compareUnsigned(%s, %s) < 0 || Long.compareUnsigned(%s, %s) > 0", expr, min, expr, max))
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < storage {
			conds = append(conds, fmt.Sprintf("(%s >>> %d) != 0", expr, f.Type.Width))
		}
		return strings.Join(conds, " || ")
	}
	return ""
}

// fixedJavaBits is the width of a Java integer storage type.
func fixedJavaBits(typ string) int {
	switch typ {
	case "byte":
		return 8
	case "short":
		return 16
	case "int":
		return 32
	}
	return 64
}

// fixedMaskLit is the long literal of the low n bits, n below 64.
func fixedMaskLit(n int) string {
	return fmt.Sprintf("0x%xL", (uint64(1)<<uint(n))-1)
}

// emitCheckWriteField emits one field's contract into checkWrite over the
// value val: `return false` on the first stored value that breaks it.
func (g *fixedGen) emitCheckWriteField(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		ind += "    "
	}
	name := javaName(f.Name)
	expr := val + "." + name
	var body strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&body, "%s"+format, append([]any{ind}, a...)...) }
	switch {
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) &&
		f.Array == ir.ArrayNone && f.KeyEnum == "":
		line("if (%sLength < 0 || %sLength > %d) { return false; }\n", expr, expr, f.Type.Size)
		switch f.Type.Kind {
		case ir.TString:
			line("if (!TableFixed.utf8WellFormed(%s, %sLength)) { return false; }\n", expr, expr)
		case ir.TWString:
			line("if (!TableFixed.wideWellFormed(%s, %sLength)) { return false; }\n", expr, expr)
		}
	default:
		count := ""
		switch {
		case f.KeyEnum != "":
			count = fmt.Sprintf("%d", f.KeyEnumRef.Max)
		case f.Array == ir.ArrayFixed:
			count = fmt.Sprintf("%d", f.ArrayBound)
		case f.Array == ir.ArrayCounted:
			line("if (%sCount < %d || %sCount > %d) { return false; }\n", expr, f.ArrayMin, expr, f.ArrayBound)
			count = expr + "Count"
		}
		if count == "" {
			if cond := fixedCheckCond(f, expr); cond != "" {
				line("if (%s) { return false; }\n", cond)
			}
			break
		}
		if cond := fixedCheckCond(f, expr+"[i]"); cond != "" {
			line("for (int i = 0; i < %s; i++) {\n", count)
			line("    if (%s) { return false; }\n", cond)
			line("}\n")
		}
	}
	if body.Len() == 0 {
		return
	}
	if f.Type.Optional {
		outer := ind[:len(ind)-4]
		g.pf("%sif (%sPresent) {\n", outer, expr)
		g.pf("%s", body.String())
		g.pf("%s}\n", outer)
		return
	}
	g.pf("%s", body.String())
}

// emitCheckWrite emits a struct's checkWrite and checkWriteBatch.
func (g *fixedGen) emitCheckWrite(st *ir.Struct) {
	g.pf("    // checkWrite is the write contract (docs/SPEC.md §5.1) over one value\n")
	g.pf("    // and everything it holds by value: false on the first field that holds\n")
	g.pf("    // what its declaration does not admit. It runs through `assert` alone,\n")
	g.pf("    // so without -ea the writer trusts.\n")
	g.pf("    static boolean checkWrite(Value v) {\n")
	for _, f := range st.Fields {
		g.emitCheckWriteField(f, "v", 8)
	}
	g.pf("        return true;\n    }\n\n")
	g.pf("    /** checkWrite over the first count values of a batch, asserted before a\n")
	g.pf("     *  save stores its first byte. */\n")
	g.pf("    public static boolean checkWriteBatch(Value[] values, int count) {\n")
	g.pf("        for (int k = 0; k < count; k++) {\n")
	g.pf("            if (values[k] != null && !checkWrite(values[k])) { return false; }\n")
	g.pf("        }\n")
	g.pf("        return true;\n    }\n\n")
}

// emitUnionCheckWrite emits a union's checkWrite: the tag inside its arms,
// then the selected arm's payload.
func (g *fixedGen) emitUnionCheckWrite(un *ir.Union) {
	g.pf("\n    /** the write contract (docs/SPEC.md §5.1): the tag names an arm, and the\n")
	g.pf("     *  selected arm holds what its declaration admits. Asserted (-ea). */\n")
	g.pf("    static boolean checkWrite(Value v) {\n")
	g.pf("        switch (v.type) {\n")
	g.pf("            case 0:\n                return true;\n")
	for i, v := range un.Variants {
		g.pf("            case %d:\n", i+1)
		if v.F != nil {
			g.emitCheckWriteField(v.F, "v", 16)
		}
		g.pf("                return true;\n")
	}
	g.pf("            default:\n                return false;\n")
	g.pf("        }\n    }\n")
}
