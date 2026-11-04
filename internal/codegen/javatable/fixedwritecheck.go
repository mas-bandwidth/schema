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
// unpaired surrogate; and, for the fixed form alone (checkWrite's `fixed`
// argument), a zero byte or a zero unit in text, which the fixed reader
// refuses as text_ill_formed. THE CHECKS ARE DEBUG ONLY: they run through
// `assert` (-ea), so a release build trusts and pays nothing. The fixed form's
// save and the message form's measureMessages and saveMessages assert
// checkWriteBatch over the whole batch before the first byte is stored, so a
// refused batch leaves the caller's buffer as it was. A NaN or an infinity is
// refused in every build by the finite pass, apart from this.
//
// MEMORY IS NOT A VALUE: a counted array's count and a text's length index the
// value's own storage, so the fixed form's save bounds them in EVERY build
// through checkStorage, a predicate apart from checkWrite, and answers -1
// before the first byte is stored rather than throwing mid-write.

// fixedCheckCond is the Java condition under which ONE stored value of f (an
// element, for an array) breaks the write contract, over expr; "" when no
// contract binds it here.
func fixedCheckCond(f *ir.Field, expr string) string {
	switch f.Type.Kind {
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return "!" + fixedClass(r.Name) + ".checkWrite(" + expr + ", fixed)"
		case *ir.Union:
			return "!" + fixedClass(r.Name) + ".checkWrite(" + expr + ", fixed)"
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
			line("if (fixed && TableFixed.holdsZero(%s, %sLength)) { return false; }\n", expr, expr)
		case ir.TWString:
			line("if (!TableFixed.wideWellFormed(%s, %sLength)) { return false; }\n", expr, expr)
			line("if (fixed && TableFixed.holdsZero(%s, %sLength)) { return false; }\n", expr, expr)
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
	g.pf("        return checkWrite(v, false);\n    }\n\n")
	g.pf("    /** checkWrite for one form: fixed adds the fixed reader's zero rule, a\n")
	g.pf("     *  zero byte or unit in text refused. */\n")
	g.pf("    static boolean checkWrite(Value v, boolean fixed) {\n")
	for _, f := range st.Fields {
		g.emitCheckWriteField(f, "v", 8)
	}
	g.pf("        return true;\n    }\n\n")
	g.pf("    /** checkWrite over the first count values of a batch, asserted before a\n")
	g.pf("     *  save stores its first byte. */\n")
	g.pf("    public static boolean checkWriteBatch(Value[] values, int count) {\n")
	g.pf("        return checkWriteBatch(values, count, false);\n    }\n\n")
	g.pf("    /** checkWriteBatch for one form; fixed as checkWrite's. */\n")
	g.pf("    public static boolean checkWriteBatch(Value[] values, int count, boolean fixed) {\n")
	g.pf("        for (int k = 0; k < count; k++) {\n")
	g.pf("            if (values[k] != null && !checkWrite(values[k], fixed)) { return false; }\n")
	g.pf("        }\n")
	g.pf("        return true;\n    }\n\n")
	g.emitCheckStorage(st.Fields)
}

// emitUnionCheckWrite emits a union's checkWrite: the tag inside its arms,
// then the selected arm's payload.
func (g *fixedGen) emitUnionCheckWrite(un *ir.Union) {
	g.pf("\n    /** the write contract (docs/SPEC.md §5.1): the tag names an arm, and the\n")
	g.pf("     *  selected arm holds what its declaration admits. Asserted (-ea). */\n")
	g.pf("    static boolean checkWrite(Value v) {\n")
	g.pf("        return checkWrite(v, false);\n    }\n\n")
	g.pf("    /** checkWrite for one form; fixed as a type's. */\n")
	g.pf("    static boolean checkWrite(Value v, boolean fixed) {\n")
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
	var arms []*ir.Field
	for _, v := range un.Variants {
		if v.F != nil {
			arms = append(arms, v.F)
		}
	}
	if !fixedHoldsStorage(arms, map[string]bool{}) {
		return
	}
	g.pf("\n    /** the SELECTED arm's counts and lengths inside its storage, in every\n")
	g.pf("     *  build. */\n")
	g.pf("    static boolean checkStorage(Value v) {\n")
	g.pf("        switch (v.type) {\n")
	for i, v := range un.Variants {
		if v.F == nil || !fixedHoldsStorage([]*ir.Field{v.F}, map[string]bool{}) {
			continue
		}
		g.pf("            case %d:\n", i+1)
		g.emitCheckStorageField(v.F, "v", 16)
		g.pf("                break;\n")
	}
	g.pf("            default:\n                break;\n")
	g.pf("        }\n        return true;\n    }\n")
}

// fixedHoldsStorage answers whether a body of these fields holds, anywhere in
// its closure by value, a counted array's count or a text's length: a number
// the writer indexes the value's own storage with.
func fixedHoldsStorage(fields []*ir.Field, seen map[string]bool) bool {
	for _, f := range fields {
		if f.Array == ir.ArrayCounted {
			return true
		}
		switch f.Type.Kind {
		case ir.TString, ir.TBytes, ir.TWString:
			return true
		case ir.TNamed:
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				if !seen[r.Name] {
					seen[r.Name] = true
					if fixedHoldsStorage(r.Fields, seen) {
						return true
					}
				}
			case *ir.Union:
				if !seen[r.Name] {
					seen[r.Name] = true
					for _, v := range r.Variants {
						if v.F != nil && fixedHoldsStorage([]*ir.Field{v.F}, seen) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// emitCheckStorage emits a struct's checkStorage, the save's EVERY-BUILD
// bound on the numbers that index the value's own storage: a counted array's
// count inside [0, N] and a text's length inside [0, N], over the value and
// everything it holds by value. A type whose closure holds neither has none.
func (g *fixedGen) emitCheckStorage(fields []*ir.Field) {
	if !fixedHoldsStorage(fields, map[string]bool{}) {
		return
	}
	g.pf("    /** every count and length this value's storage is indexed by lies inside\n")
	g.pf("     *  that storage: not an assert, the save answers -1 for it in every build\n")
	g.pf("     *  before the first byte is stored. */\n")
	g.pf("    static boolean checkStorage(Value v) {\n")
	for _, f := range fields {
		if fixedHoldsStorage([]*ir.Field{f}, map[string]bool{}) {
			g.emitCheckStorageField(f, "v", 8)
		}
	}
	g.pf("        return true;\n    }\n\n")
}

// emitCheckStorageField bounds one field's count or length and walks the
// stored elements that hold one of their own: the present payload of an
// optional, every slot of a fixed array, a counted array's live count.
func (g *fixedGen) emitCheckStorageField(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	expr := val + "." + javaName(f.Name)
	if f.Type.Optional {
		g.pf("%sif (%sPresent) {\n", ind, expr)
		ind += "    "
	}
	switch {
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) &&
		f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("%sif (%sLength < 0 || %sLength > %d) { return false; }\n", ind, expr, expr, f.Type.Size)
	default:
		count := ""
		switch {
		case f.KeyEnum != "":
			count = fmt.Sprintf("%d", f.KeyEnumRef.Max)
		case f.Array == ir.ArrayFixed:
			count = fmt.Sprintf("%d", f.ArrayBound)
		case f.Array == ir.ArrayCounted:
			g.pf("%sif (%sCount < 0 || %sCount > %d) { return false; }\n", ind, expr, expr, f.ArrayBound)
			count = expr + "Count"
		}
		test := ""
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				if fixedHoldsStorage(r.Fields, map[string]bool{}) {
					test = fixedClass(r.Name) + ".checkStorage"
				}
			case *ir.Union:
				var arms []*ir.Field
				for _, v := range r.Variants {
					if v.F != nil {
						arms = append(arms, v.F)
					}
				}
				if fixedHoldsStorage(arms, map[string]bool{}) {
					test = fixedClass(r.Name) + ".checkStorage"
				}
			}
		}
		switch {
		case test == "":
		case count == "":
			g.pf("%sif (!%s(%s)) { return false; }\n", ind, test, expr)
		default:
			g.pf("%sfor (int i = 0; i < %s; i++) {\n", ind, count)
			g.pf("%s    if (!%s(%s[i])) { return false; }\n", ind, test, expr)
			g.pf("%s}\n", ind)
		}
	}
	if f.Type.Optional {
		g.pf("%s}\n", ind[:len(ind)-4])
	}
}
