package darttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rideOpen opens the block that writes a field when it rides. A required
// field always rides, so its block is a plain one: an `if (true)` is a
// constant condition, and its element elision rules stay intact inside.
func rideOpen(ind string, f *ir.Field, condition string) string {
	if f.Required {
		return ind + "{\n"
	}
	return ind + "if (" + condition + ") {\n"
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
		cond := "!" + seen + ".contains(" + viewDartString(ir.TableFieldJsonKey(f)) + ")"
		if wire {
			cond = fmt.Sprintf("%s[%d] == 0", seen, i)
		}
		if guard := g.guardCond(st, guards[f.Name]); guard != "" {
			cond = "(" + guard + ") && " + cond
		}
		g.pf("%sif (%s) missing.add(%s);\n", ind, cond, viewDartString(f.Name))
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

// writeCheckLine is one write check: ok is the condition a value that may be
// written keeps. With no refusal it is a debug assert (Dart write checks run in
// debug builds only, as the save asserts do); with one it refuses in every
// build.
func writeCheckLine(ind, ok, refusal string) string {
	if refusal == "" {
		return fmt.Sprintf("%sassert(%s);\n", ind, ok)
	}
	return fmt.Sprintf("%sif (!(%s)) { %s }\n", ind, ok, refusal)
}

// writeCheckLoop runs body over each element of expr. Under asserts only, the
// loop itself rides inside an assert, so a release build walks nothing.
func writeCheckLoop(ind, name, expr, body, refusal string) string {
	if refusal == "" {
		return fmt.Sprintf("%sassert(() {\n%sfor (final %s in %s) {\n%s%s}\n%sreturn true;\n%s}());\n", ind, ind, name, expr, body, ind, ind, ind)
	}
	return fmt.Sprintf("%sfor (final %s in %s) {\n%s%s}\n", ind, name, expr, body, ind)
}

// requiredRangeCheck holds a required ranged field to its range before it is
// written. cell is the library's reusable Float32List(1) a float32 narrows
// through; refusal "" makes the check a debug assert.
func requiredRangeCheck(f *ir.Field, expr, ind, cell, refusal string) string {
	if !f.Required {
		return ""
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		inner := *singleOf(f)
		inner.Required = true
		body := requiredRangeCheck(&inner, "requiredElement", ind+"\t", cell, refusal)
		if body == "" {
			return ""
		}
		return writeCheckLoop(ind, "requiredElement", expr, body, refusal)
	}
	var cond string
	if f.HasFloatRange {
		cond = floatRangeCond(f, expr, cell)
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
	return writeCheckLine(ind, cond, refusal)
}

// jsonWriteCheck holds a field the JSON writer is about to write to its
// declared bounds. Dart write checks are debug asserts (refusal ""); cell is
// the library's reusable Float32List(1) a float32 narrows through.
func jsonWriteCheck(f *ir.Field, expr, ind, cell, refusal string) string {
	if f.Type.Optional {
		return ""
	}
	var out string
	if f.Array == ir.ArrayCounted {
		out += writeCheckLine(ind, fmt.Sprintf("%s.length >= %d && %s.length <= %d", expr, f.ArrayMin, expr, f.ArrayBound), refusal)
	} else if f.Array == ir.ArrayList && f.ArrayBound > 0 && !f.IsTextList() {
		out += writeCheckLine(ind, fmt.Sprintf("%s.length <= %d", expr, f.ArrayBound), refusal)
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		if f.IsTextList() {
			return out
		}
		inner := *singleOf(f)
		body := jsonWriteCheck(&inner, "jsonElement", ind+"\t", cell, refusal)
		if body != "" {
			out += writeCheckLoop(ind, "jsonElement", expr, body, refusal)
		}
		return out
	}
	if f.Type.Kind == ir.TString && !f.Type.Blob() && !f.Type.TextUnbounded() && f.Type.Size > 0 {
		return writeCheckLine(ind, fmt.Sprintf("utf8.encode(%s).length <= %d", expr, f.Type.Size), refusal)
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() && f.Type.Size > 0 {
		return writeCheckLine(ind, fmt.Sprintf("%s.length <= %d", expr, f.Type.Size), refusal)
	}
	var cond string
	switch {
	case f.HasFloatRange:
		cond = floatRangeCond(f, expr, cell)
	case f.Type.Kind == ir.TBits && f.Type.Width < 64:
		cond = fmt.Sprintf("(%s & ~%s) == 0", expr, dartU64Literal((uint64(1)<<f.Type.Width)-1))
	case f.HasIntRange || f.Type.Kind == ir.TFixed:
		lo, hi, ok := ir.TableRawRange(f)
		if ok {
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
	}
	if cond != "" {
		out += writeCheckLine(ind, cond, refusal)
	}
	return out
}

// floatRangeCond is the condition a ranged float keeps to be written: a
// float32 NARROWED FIRST (docs/SPEC-TABLES.md §3.3, THE RANGE IS HELD AT
// FLOAT32) through cell, the library's one reusable Float32List(1) (store
// the value, read [0]: no allocation per check), then held to the float32
// nearest each declared end, so a double that rounds to an end passes exactly
// as the value the store writes does; a float64 as it is, to its declared ends.
// A NaN fails both comparisons.
func floatRangeCond(f *ir.Field, expr, cell string) string {
	lo, hi, _ := fixedFloatClampEnds(f)
	if f.Type.Kind == ir.TFloat32 {
		return fmt.Sprintf("(%s..[0] = %s)[0] >= %s && %s[0] <= %s", cell, expr, lo, cell, hi)
	}
	return fmt.Sprintf("%s >= %s && %s <= %s", expr, lo, expr, hi)
}
