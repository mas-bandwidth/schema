package darttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strconv"
)

// Required fields always ride, while their element elision rules stay intact.
func requiredRide(f *ir.Field, condition string) string {
	if f.Required {
		return "true"
	}
	return condition
}
func hasRequired(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Required {
			return true
		}
	}
	return false
}
func (g *wireGen) emitRequiredCheck(st *ir.Struct, wire bool, ind string) {
	if !hasRequired(st) {
		return
	}
	report := "r"
	seen := "seen"
	if wire {
		report = "r.report"
		seen = "requiredSeen"
	}
	g.pf("%sfinal missing = <String>[];\n", ind)
	guards := wireGuards(st)
	for i, f := range st.Fields {
		if !f.Required {
			continue
		}
		cond := "!" + seen + ".contains(" + strconv.Quote(ir.TableFieldJsonKey(f)) + ")"
		if wire {
			cond = fmt.Sprintf("%s[%d] == 0", seen, i)
		}
		if guard := g.guardCond(st, guards[f.Name]); guard != "" {
			cond = "(" + guard + ") && " + cond
		}
		g.pf("%sif (%s) missing.add(%s);\n", ind, cond, strconv.Quote(f.Name))
	}
	if wire {
		g.pf("%sif (missing.isNotEmpty && r.report.missingFields.isEmpty) { r.report.missingFields = missing.join(', '); r.report.refused = true; throw const _TableMissingRequired(); }\n", ind)
	} else {
		g.pf("%sif (missing.isNotEmpty) { %s.missingFields = missing.join(', '); throw FormatException('missing_required: '+%s.missingFields); }\n", ind, report, report)
	}
}

// The same compatibility rule the value readers use, including collection
// element kinds. A malformed payload still receives its ordinary wire verdict.
func requiredKindCondition(f *ir.Field) string {
	kind := ir.TableWireFieldKind(f)
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		kind = wireKindPtr
	}
	if f.Type.Kind == ir.TBytes {
		kind = wireKindArray
	}
	outer := fmt.Sprintf("(kind == %d || _kindWidens(kind, %d))", kind, kind)
	if f.IsMap() || f.Array != ir.ArrayNone || f.KeyEnum != "" || f.Type.Kind == ir.TBytes {
		elem := ir.TableWireElemKind(f)
		if f.IsMap() {
			elem = wireKindTable
		}
		if f.Type.Kind == ir.TBytes {
			elem = wireKindU8
		}
		return fmt.Sprintf("kind == %d && _requiredCollectionKind(r, %d)", kind, elem)
	}
	return outer
}

const requiredWireRuntime = `
final class _TableMissingRequired implements Exception { const _TableMissingRequired(); }
bool _requiredCollectionKind(TableWireReader r, int expected) {
 final peek=TableWireReader.sub(r,r.pos,r.limit);
 final length=peek.readLeb();
 if(length==null||!peek.has(length))return false;
 if(length<2)return true;
 final kind=peek.readByte();
 return kind==expected||_kindWidens(kind,expected);
}
`

func requiredRangeCheck(f *ir.Field, expr, ind, refusal string) string {
	if !f.Required {
		return ""
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		inner := *singleOf(f)
		inner.Required = true
		body := requiredRangeCheck(&inner, "requiredElement", ind+"\t", refusal)
		if body == "" {
			return ""
		}
		return fmt.Sprintf("%sfor (final requiredElement in %s) {\n%s%s}\n", ind, expr, body, ind)
	}
	var cond string
	if f.HasFloatRange {
		cond = fmt.Sprintf("%s >= %s && %s <= %s", expr, fmtFloat(f.FMin), expr, fmtFloat(f.FMax))
	} else if f.HasIntRange {
		lo, hi, _ := ir.TableRawRange(f)
		switch {
		case isWide(f):
			cond = fmt.Sprintf("%s >= %s && %s <= %s", expr, bigLiteral(lo), expr, bigLiteral(hi))
		case f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 64:
			value := "BigInt.from(" + expr + ").toUnsigned(64)"
			cond = fmt.Sprintf("%s >= %s && %s <= %s", value, bigLiteral(lo), value, bigLiteral(hi))
		default:
			cond = fmt.Sprintf("%s >= %s && %s <= %s", expr, dartBoundLiteral(lo), expr, dartBoundLiteral(hi))
		}
	}
	if cond == "" {
		return ""
	}
	return fmt.Sprintf("%sif (!(%s)) { %s }\n", ind, cond, refusal)
}
