package lobstertable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("fixed-record", emitFixedRecords) }

// The generic Record API is singular. Its fixed adapter owns one native row
// and projects storage around the same checked save and strict lineage load
// used by typed callers. No wire facts are re-derived here.
func emitFixedRecords(g *lobster.Gen) error {
	for _, st := range g.File.Tables {
		if !st.FixedDeclared || !fixedTableQualifies(g, st) {
			continue
		}
		g.Import("load")
		name := fixedName(st)
		g.Pf("class %sRecordCodec : schema_rt.FixedRecordCodec\n", name)
		g.Pf("    def read_fixed(data:string, report:schema_rt.LoadReport) -> schema_rt.Record?:\n")
		g.Pf("        let rows = [New%s()]\n", st.Name)
		g.Pf("        let plan:[schema_rt.TableFixedEntry] = []\n")
		g.Pf("        for(%sPlanMax): push(plan, schema_rt.TableFixedEntry {})\n", name)
		g.Pf("        let native = schema_rt.TableReport {}\n")
		g.Pf("        let n = %sLoad(rows, data, plan, native)\n", name)
		g.Pf("        if n != 1:\n")
		g.Pf("            schema_rt.read_refuse(report, if native.reason != \"\": native.reason else: \"record_count\")\n")
		g.Pf("            return nil\n")
		g.Pf("        let rec = schema_rt.new_record(%sDescriptor)\n", st.Name)
		p := fixedRecordProjection{g: g}
		p.record(st, "rows[0]", "rec", "        ")
		g.Pf("        return rec\n")
		g.Pf("    def write_fixed(rec:schema_rt.Record) -> string, string:\n")
		g.Pf("        let row = New%s()\n", st.Name)
		p = fixedRecordProjection{g: g, toTyped: true}
		p.record(st, "row", "rec", "        ")
		g.Pf("        let n, wire, why = %sSaveChecked([row], schema_rt.zeros(%sMeasure(1)))\n", name, name)
		g.Pf("        return (if n < 0: \"\" else: wire), why\n\n")
		g.Pf("schema_rt.register_fixed_record_codec(%sRecordCodec { name: %q })\n\n", name, st.Name)
	}
	return nil
}

type fixedRecordProjection struct {
	g       *lobster.Gen
	toTyped bool
	seq     int
}

func (p *fixedRecordProjection) local() string {
	p.seq++
	return fmt.Sprintf("bridge_%d", p.seq)
}

func (p *fixedRecordProjection) check(test, ind string) {
	if p.toTyped {
		p.g.Pf("%sif %s: return \"\", \"invalid_value\"\n", ind, test)
	}
}

func (p *fixedRecordProjection) assign(typed, cell, ind string) {
	if p.toTyped {
		p.g.Pf("%s%s = %s\n", ind, typed, cell)
	} else {
		p.g.Pf("%s%s = %s\n", ind, cell, typed)
	}
}

func (p *fixedRecordProjection) record(st *ir.Struct, typed, rec, ind string) {
	p.check(fmt.Sprintf("%s.d.name != %q or length(%s.fields) != %d", rec, st.Name, rec, len(st.Fields)), ind)
	for i, f := range st.Fields {
		sl := p.local()
		p.g.Pf("%slet %s = %s.fields[%d]\n", ind, sl, rec, i)
		p.check(fmt.Sprintf("%s.fd.id != %s", sl, idLit(ir.TableWireId(ir.TableFieldWireName(f)))), ind)
		if p.toTyped && f.Deprecated {
			continue // the fresh native row already holds the declared default
		}
		p.field(f, typed+"."+ir.GoExportName(f.Name), sl, ind)
	}
}

func (p *fixedRecordProjection) field(f *ir.Field, typed, sl, ind string) {
	if f.Type.Optional {
		present := slotExprFull(typed, f, ir.GoExportName(f.Name+"_present"))
		p.assign(present, sl+".present", ind)
		p.g.Pf("%sif %s:\n", ind, present)
		ind += "    "
	}
	if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
		n := fmt.Sprint(f.ArrayBound)
		if f.KeyEnumRef != nil {
			n = fmt.Sprint(f.KeyEnumRef.Max)
		}
		if f.Array == ir.ArrayCounted {
			n = slotExpr(typed, f, "Count")
			p.assign(n, sl+".count", ind)
			p.check(fmt.Sprintf("%s < %d or %s > %d", n, f.ArrayMin, n, f.ArrayBound), ind)
		}
		p.check(fmt.Sprintf("length(%s.elems) < %s", sl, n), ind)
		i := p.local()
		p.g.Pf("%sfor(%s) %s:\n", ind, n, i)
		p.element(f, typed+"["+i+"]", sl+".elems["+i+"]", ind+"    ")
		return
	}
	p.element(f, typed, sl+".cell", ind)
	if !p.toTyped && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) {
		p.g.Pf("%s%s.count = %s\n", ind, sl, slotExpr(typed, f, "Length"))
	}
}

func (p *fixedRecordProjection) element(f *ir.Field, typed, cell, ind string) {
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Struct:
			r := p.local()
			if !p.toTyped {
				p.g.Pf("%slet %s = schema_rt.new_record(schema_rt.table_of(%q))\n", ind, r, ref.Name)
				p.g.Pf("%s%s.tab = %s\n", ind, cell, r)
				p.record(ref, typed, r, ind)
				return
			}
			p.g.Pf("%slet %s = %s.tab\n", ind, r, cell)
			p.check("not "+r, ind)
			p.record(ref, typed, r, ind)
		case *ir.Union:
			p.union(ref, typed, cell, ind)
		case *ir.Enum:
			if p.toTyped {
				p.g.Pf("%s%s = %s(%s.i)\n", ind, typed, p.g.TypeRef(ref.Name, false), cell)
			} else {
				p.g.Pf("%s%s.i = int(%s)\n", ind, cell, typed)
			}
		case *ir.Flags:
			p.assign(typed, cell+".i", ind)
		}
	case ir.TBool:
		p.assign(typed, cell+".b", ind)
	case ir.TFloat32, ir.TFloat64:
		p.assign(typed, cell+".f", ind)
	case ir.TString, ir.TBytes, ir.TWString:
		length := slotExpr(typed, f, "Length")
		value := cell + ".s"
		if t.Kind == ir.TWString {
			value = cell + ".units"
		}
		if p.toTyped {
			p.g.Pf("%s%s = length(%s)\n", ind, length, value)
			p.check(fmt.Sprintf("%s > %d", length, t.Size), ind)
			if t.Kind == ir.TWString {
				i := p.local()
				p.g.Pf("%sfor(%s) %s:\n", ind, length, i)
				p.check(fmt.Sprintf("%s[%s] < 0 or %s[%s] > 65535", value, i, value, i), ind+"    ")
				p.g.Pf("%s    %s = write_int16_le(%s, %s * 2, %s[%s])\n", ind, typed, typed, i, value, i)
			} else {
				p.g.Pf("%s%s = write_substring(%s, 0, %s, false)\n", ind, typed, typed, value)
			}
		} else if t.Kind == ir.TWString {
			i := p.local()
			p.g.Pf("%s%s = []\n", ind, value)
			p.g.Pf("%sfor(%s) %s: push(%s, read_uint16_le(%s, %s * 2))\n", ind, length, i, value, typed, i)
		} else {
			p.g.Pf("%s%s = substring(%s, 0, %s)\n", ind, value, typed, length)
		}
	case ir.TInt, ir.TFixed, ir.TBits:
		if t.Width > 64 {
			p.assign(typed, cell+".w", ind)
		} else if t.Kind == ir.TFixed {
			if p.toTyped {
				hi := "0"
				if t.Signed {
					hi = "(if " + cell + ".w.lo < 0: -1 else: 0)"
				}
				p.check(cell+".w.hi != "+hi, ind)
				p.assign(typed, cell+".w.lo", ind)
			} else {
				hi := "0"
				if t.Signed {
					hi = "if " + typed + " < 0: -1 else: 0"
				}
				p.g.Pf("%s%s.w = schema_rt.Int128 { lo: %s, hi: %s }\n", ind, cell, typed, hi)
			}
		} else {
			p.assign(typed, cell+".i", ind)
		}
	}
}

func (p *fixedRecordProjection) union(un *ir.Union, typed, cell, ind string) {
	if p.toTyped {
		p.check(fmt.Sprintf("%s.i < 0 or %s.i > %d", cell, cell, len(un.Variants)), ind)
		p.g.Pf("%s%s.Type = %sType(%s.i)\n", ind, typed, p.g.TypeRef(un.Name, p.g.Unit.TableUnions[un.Name] != nil), cell)
	} else {
		p.g.Pf("%s%s.i = int(%s.Type)\n", ind, cell, typed)
	}
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		p.g.Pf("%sif %s.i == %d:\n", ind, cell, i+1)
		f := *arm.F
		f.Name = arm.Name
		expr := typed + "." + ir.GoExportName(arm.Name)
		if arm.Body() {
			p.element(&f, expr, cell, ind+"    ")
		} else {
			sl := p.local()
			if p.toTyped {
				p.g.Pf("%s    let %s = %s.arm\n", ind, sl, cell)
				p.check("not "+sl, ind+"    ")
				p.field(&f, expr, sl, ind+"    ")
			} else {
				ud := p.local()
				p.g.Pf("%s    let %s = schema_rt.lookup_union(%q)\n", ind, ud, un.Name)
				p.g.Pf("%s    assert %s\n", ind, ud)
				p.g.Pf("%s    let %s = schema_rt.new_slot(%s.arms[%d])\n", ind, sl, ud, i)
				p.g.Pf("%s    %s.arm = %s\n", ind, cell, sl)
				p.field(&f, expr, sl, ind+"    ")
			}
		}
	}
}
