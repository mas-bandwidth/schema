package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITE CONTRACT OF THE FIXED AND MESSAGE FORMS (docs/SPEC.md §5.1): a
// save refuses a value its declaration does not admit. <T>FixedCheckBody is
// that contract over one value and everything it holds by value, false on the
// first field that breaks it: a ranged integer off its min/max (scalars and
// every stored element), a bits(N) value wider than N, an enum ordinal outside
// its set, a union tag outside its arms, a counted array's count or a text's
// length outside its bounds, narrow text that is not well-formed UTF-8, and
// wide text with an unpaired surrogate. THE CHECKS ARE DEBUG ONLY: the fixed
// form's save and the message form's MeasureMessages and SaveMessages run them
// inside one Dart assert over the whole batch, before the first byte is
// stored, so a release AOT build pays nothing and a refused batch leaves the
// caller's buffer as it was. A NaN or an infinity is refused in every build by
// the finite pass, apart from this.

// fixedCheckHolds is the Dart condition ONE stored scalar value of f must meet,
// over expr; "" when no contract binds it here.
func fixedCheckHolds(f *ir.Field, expr string) string {
	if e := enumRef(f); e != nil {
		return fmt.Sprintf("%s >= 0 && %s <= %d", expr, expr, len(e.Variants))
	}
	if f.Type.Kind != ir.TInt && f.Type.Kind != ir.TBits {
		return ""
	}
	if isWide(f) || f.Type.Width > 64 {
		return ""
	}
	if f.HasIntRange {
		lo, hi, ok := ir.TableRawRange(f)
		if !ok {
			return ""
		}
		if f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 64 {
			value := "BigInt.from(" + expr + ").toUnsigned(64)"
			return fmt.Sprintf("%s >= %s && %s <= %s", value, bigLiteral(lo), value, bigLiteral(hi))
		}
		return fmt.Sprintf("%s >= %s && %s <= %s", expr, dartBoundLiteral(lo), expr, dartBoundLiteral(hi))
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		return fmt.Sprintf("%s >= 0 && %s <= %d", expr, expr, (uint64(1)<<uint(f.Type.Width))-1)
	}
	return ""
}

// emitCheckBody emits <T>FixedCheckBody for one type of the closure; a type
// that binds no contract still carries one, which answers true.
func (g *fixedGen) emitCheckBody(st *ir.Struct) {
	g.cur = st
	g.pf("/// %s's write contract (docs/SPEC.md §5.1): false on the first field\n", st.Name)
	g.pf("/// that holds what its declaration does not admit. A save asserts it over\n")
	g.pf("/// the whole batch before the first byte is stored.\n")
	g.fn("bool", lowerFirst(st.Name)+"FixedCheckBody", []string{st.Name + " value"})
	for _, f := range st.Fields {
		g.emitCheckField(f, "value."+dartName(f.Name), "  ")
	}
	g.pf("  return true;\n}\n\n")
}

// emitCheckField tests one field line: the present payload of an optional.
func (g *fixedGen) emitCheckField(f *ir.Field, expr, ind string) {
	if f.Type.Optional {
		g.pf("%sif (%sPresent) {\n", ind, expr)
		g.emitCheckPayload(f, expr, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitCheckPayload(f, expr, ind)
}

func (g *fixedGen) emitCheckPayload(f *ir.Field, expr, ind string) {
	loop := func(count string) {
		g.pf("%sfor (var i = 0; i < %s; i++) {\n", ind, count)
		g.emitCheckElement(f, expr+"[i]", ind+"  ")
		g.pf("%s}\n", ind)
	}
	switch {
	case f.KeyEnum != "":
		loop(fmt.Sprintf("%d", f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		loop(fmt.Sprintf("%d", f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		count := fixedDartSibling(expr, "Count")
		g.pf("%sif (%s < %d || %s > %d) return false;\n", ind, count, f.ArrayMin, count, f.ArrayBound)
		loop(count)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		length := fixedDartSibling(expr, "Length")
		g.pf("%sif (%s < 0 || %s > %d) return false;\n", ind, length, length, f.Type.Size)
		switch f.Type.Kind {
		case ir.TString:
			g.pf("%sif (!tableFixedUtf8WellFormed(%s, %s)) return false;\n", ind, expr, length)
		case ir.TWString:
			g.pf("%sif (!tableFixedWideWellFormed(%s, %s)) return false;\n", ind, expr, length)
		}
	default:
		g.emitCheckElement(f, expr, ind)
	}
}

func (g *fixedGen) emitCheckElement(f *ir.Field, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sif (!%sFixedCheckBody(%s)) return false;\n", ind, lowerFirst(r.Name), expr)
			return
		case *ir.Union:
			g.pf("%sswitch (%s.type) {\n", ind, expr)
			g.pf("%s  case 0:\n%s    break;\n", ind, ind)
			for i, v := range r.Variants {
				g.pf("%s  case %d:\n", ind, i+1)
				if v.F != nil {
					arm := *v.F
					arm.Type.Optional = false
					g.emitCheckPayload(&arm, expr+"."+dartName(v.Name), ind+"    ")
				}
				g.pf("%s    break;\n", ind)
			}
			g.pf("%s  default:\n%s    return false;\n%s}\n", ind, ind, ind)
			return
		}
	}
	if holds := fixedCheckHolds(f, expr); holds != "" {
		g.pf("%sif (!(%s)) return false;\n", ind, holds)
	}
}

// fixedBatchCheck is the assert a save makes over its batch before the first
// byte is stored; values and count name the batch.
func fixedBatchCheck(st *ir.Struct, each string) string {
	return fmt.Sprintf("  assert(%s.every(%sFixedCheckBody), 'write contract: %s');\n", each, lowerFirst(st.Name), st.Name)
}
