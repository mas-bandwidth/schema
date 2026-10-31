package mootable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TEXT FORM (docs/SPEC-TABLES.md §16; docs/PORT-MOO.md, THE LAYERS, layer
// 4). runtime/json.moo is ONE WALK over the descriptors, held to the compiler's
// own engine (internal/tabletext) byte for byte and counter for counter; this
// part joins every table module to it with three calls per table that carries
// the form, and hands the walk the two facts a descriptor does not carry:
//
//   - a flags set's variant NAMES (a flags field's descriptor is its uint64
//     storage, and the text spells the set by name, §16.2);
//   - a text list's ELEMENT (§2.13): the record its storage holds, whether
//     the element is optional, and its byte bound, since the text is a plain
//     array of strings over records.
//
// The surface, per table carrying json (docs/SPEC-TABLES.md §16.1):
//
//	<Name>FromJson(value, text, report?) -> boolean   one instance from one text
//	<Name>ToJsonMeasure(value) -> number               exact bytes; -1 refused
//	<Name>ToJson(value, out: buffer) -> number         the size; -1 refused or too small
//
// moo has no float: every conversion in the walk is exact integer arithmetic
// (runtime/json.moo says how), and no value in it ever passes through a double.
func init() {
	Register("json", jsonPart)
}

// unitCarriesJson is the unit-level question runtimeGate asks: whether
// json.moo is emitted at all (docs/SPEC-TABLES.md §2.12).
func unitCarriesJson(u *ir.Unit) bool {
	for _, t := range u.Tables {
		if !t.IsMapEntry() && t.CarriesForm("json") {
			return true
		}
	}
	return false
}

func jsonPart(g *Gen) error {
	if !unitCarriesJson(g.Unit) {
		return nil
	}
	reach := closure(g.Unit)
	var extras strings.Builder
	extra := func(path string, f *ir.Field) {
		if fl, ok := f.Type.Ref.(*ir.Flags); ok && f.Type.Kind == ir.TNamed {
			names := make([]string, len(fl.Variants))
			for i, v := range fl.Variants {
				names[i] = fmt.Sprintf("%q", v)
			}
			target := path
			if f.Array != ir.ArrayNone {
				target += ".e"
			}
			fmt.Fprintf(&extras, "_json.extra(%s, { flags = { %s } })\n", target, strings.Join(names, ", "))
		}
		if f.IsTextList() {
			fmt.Fprintf(&extras, "_json.extra(%s, { tl = { opt = %v, bound = %d, rec = %q } })\n",
				path, ir.TextListOptional(f), ir.TextListValue(f).Type.Size, f.TextListElem().Name)
		}
	}
	structExtras := func(d *ir.Struct) {
		for i, f := range d.Fields {
			extra(fmt.Sprintf("%s.%sDescriptor.fields[%d]", g.Self, d.Name, i+1), f)
		}
	}
	unionExtras := func(un *ir.Union) {
		for i, v := range un.Variants {
			if v.Void() {
				continue
			}
			extra(fmt.Sprintf("%s.%sDescriptor.arms[%d]", g.Self, un.Name, i+1), armField(v))
		}
	}
	for _, un := range g.File.TableUnions {
		unionExtras(un)
	}
	for _, t := range g.File.Tables {
		structExtras(t)
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable && reach[d.Name] {
				structExtras(d)
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil && reach[d.Name] {
				unionExtras(d)
			}
		}
	}
	j := g.Require("json")
	g.Pf("-- THE TEXT FORM (docs/SPEC-TABLES.md §16): the facts runtime/json.moo's walk\n")
	g.Pf("-- needs beyond the descriptors — flags names, text list elements\n")
	g.Pf("%s", strings.ReplaceAll(extras.String(), "_json.", j+"."))
	g.Pf("\n")
	for _, t := range g.File.Tables {
		if t.IsMapEntry() || t.IsTextListElem() || !t.CarriesForm("json") {
			continue
		}
		d := fmt.Sprintf("%s.%sDescriptor", g.Self, t.Name)
		g.Pf("-- %sFromJson fills ONE %s from ONE text (docs/SPEC-TABLES.md §16.1): false,\n", t.Name, t.Name)
		g.Pf("-- with report.malformed set, for a text that is not JSON or that a rule\n")
		g.Pf("-- refuses by name, and the value is then reset to its declared defaults.\n")
		g.Pf("function %s.%sFromJson(value: %s, text: string | buffer, report: any?): boolean\n", g.Self, t.Name, t.Name)
		g.Pf("\treturn %s.fromJson(%s, value, text, report)\nend\n\n", j, d)
		g.Pf("-- %sToJsonMeasure is the exact size %sToJson writes, or -1 for a value\n", t.Name, t.Name)
		g.Pf("-- no text spells (docs/SPEC-TABLES.md §16.3).\n")
		g.Pf("function %s.%sToJsonMeasure(value: %s): number\n", g.Self, t.Name, t.Name)
		g.Pf("\treturn %s.toJsonMeasure(%s, value)\nend\n\n", j, d)
		g.Pf("-- %sToJson writes the canonical text into out and returns its size, or -1\n", t.Name)
		g.Pf("-- for a value no text spells or an out smaller than the measure.\n")
		g.Pf("function %s.%sToJson(value: %s, out: buffer): number\n", g.Self, t.Name, t.Name)
		g.Pf("\treturn %s.toJsonInto(%s, value, out)\nend\n\n", j, d)
	}
	return nil
}
