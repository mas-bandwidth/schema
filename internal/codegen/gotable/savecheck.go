package gotable

import (
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// textWriteCheckBody is the SAVE's TEXT PREFLIGHT: ill-formed UTF-8 or an
// unpaired surrogate is refused before the first byte of the caller's buffer is
// touched, so a refused Save writes nothing (docs/SPEC.md §5.1). It is the same
// rule SaveBody applies while writing, read over the value alone. It folds onto
// the Save's existing declaration line, so the generated source does not grow.
func (g *tableGen) textWriteCheckBody(st *ir.Struct) string {
	var b strings.Builder
	out := g.out
	g.out = &b
	g.emitTextWriteCheck(st)
	g.out = out
	return b.String()
}

func (g *tableGen) emitTextWriteCheck(st *ir.Struct) {
	guards := tableGuardExprs(st)
	var bad []string
	for _, f := range st.Fields {
		if !fieldHasTextCheck(f) {
			continue
		}
		cond := guards[f.Name]
		if f.Type.Optional {
			present := "value." + member(f) + "Present"
			if cond != "" {
				cond = "(" + cond + ") && " + present
			} else {
				cond = present
			}
		}
		expr := "value." + member(f)
		check := expr + "Length < 0 || " + expr + "Length > " + strconv.Itoa(int(f.Type.Size)) + " || "
		if f.Type.Kind == ir.TString {
			check += "!tableUTF8Valid(" + expr + "[:" + expr + "Length])"
		} else {
			check += "!tableUtf16UnitsValid(" + expr + "[:" + expr + "Length])"
		}
		if cond != "" {
			check = "!(" + cond + ") || (" + check + ")"
		}
		bad = append(bad, "("+check+")")
	}
	if len(bad) == 0 {
		return
	}
	g.pf("\tif %s { return -1 }\n", strings.Join(bad, " || "))
}

func fieldHasTextCheck(f *ir.Field) bool {
	if f.Array != ir.ArrayNone || f.IsList() || f.IsMap() || f.Type.Pointer || f.Type.TextUnbounded() {
		return false
	}
	return f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString
}

func tableDirectText(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fieldHasTextCheck(f) {
			return true
		}
	}
	return false
}

// emitTableSave emits the buffer-level entry of the measure/save pair: <X>Save
// writes into a caller-provided buffer and returns the bytes written, or -1
// when the value is invalid or the buffer is too small. A refused Save writes
// nothing: its text preflight runs before the form byte.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	check := g.textWriteCheckBody(st)
	if st.Integrity != "" {
		g.pf("func %sSave(value *%s, buffer []byte) int64 {\n%s if len(buffer) < 4 { return -1 }\n var ids TableIds\n w := TableWriter{Buffer:buffer[:len(buffer)-4], Ids:&ids}\n w.Put8(1)\n if !%sSaveBody(&w,value) { return -1 }; w.Trailer()\n if w.Overflow || ids.Overflow { return -1 }; return tableIntegritySeal(buffer, w.Offset)\n}\n\n", st.Name, g.storageName(st.Name), check, st.Name)
		return
	}
	g.pf("func %sSave(value *%s, buffer []byte) int64 {\n%s var ids TableIds\n w := TableWriter{Buffer:buffer, Ids:&ids}\n w.Put8(1)\n if !%sSaveBody(&w,value) { return -1 }; w.Trailer()\n if w.Overflow || ids.Overflow { return -1 }; return w.Offset\n}\n\n", st.Name, g.storageName(st.Name), check, st.Name)
}
