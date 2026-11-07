package mootable

import "github.com/mas-bandwidth/schema/v2/ir"

// THE BASE64 CARRIER (docs/SPEC-TABLES.md §16.8): EVERY TABLE HAS IT, fixed
// and variable alike, over the wire its Save writes. runtime/textcarrier.moo
// holds the canonical encode and the two refusal names; this part joins each
// table to it with the table's own Save and strict read (runtime/table_save.moo,
// table_load.moo):
//
//	<Name>SaveText(value) -> (string?, refusal?)    the base64 of Save's bytes
//	<Name>LoadText(text, report?) -> (value?, name?) ReadStrict over the decoded
//	                                                 wire; base64_damaged and
//	                                                 base64_not_canonical first
func init() {
	Register("text", textPart)
}

func textPart(g *Gen) error {
	var roots []*ir.Struct
	for _, t := range g.File.Tables {
		if t.IsMapEntry() || t.IsTextListElem() {
			continue
		}
		roots = append(roots, t)
	}
	if len(roots) == 0 {
		return nil
	}
	carrier, save, load := g.Require("textcarrier"), g.Require("table_save"), g.Require("table_load")
	for _, t := range roots {
		g.Pf("-- %sSaveText is the canonical base64 of %s's wire (docs/SPEC-TABLES.md §16.8),\n", t.Name, t.Name)
		g.Pf("-- or nil and Save's own refusal.\n")
		g.Pf("function %s.%sSaveText(value: %s): (string?, any)\n", g.Self, t.Name, t.Name)
		g.Pf("\treturn %s.saveText(function(v: any): (buffer?, any)\n\t\treturn %s.save(%s.%sDescriptor, v)\n\tend, value)\nend\n\n", carrier, save, g.Self, t.Name)
		g.Pf("-- %sLoadText is the strict read (§6.5) of the wire a text spells: nil and\n", t.Name)
		g.Pf("-- base64_damaged or base64_not_canonical for a body that is not the canonical\n")
		g.Pf("-- spelling of its bytes, else the strict read's value or its refusal.\n")
		g.Pf("function %s.%sLoadText(text: string, report: any?): (%s?, string?)\n", g.Self, t.Name, t.Name)
		g.Pf("\treturn %s.loadText(text, function(wire: buffer, rep: any): (any, string?)\n", carrier)
		g.Pf("\t\tlocal r: any = rep or %s.newReport()\n\t\tlocal v = %s.ReadStrict(%q, wire, r)\n", load, load, t.Name)
		g.Pf("\t\treturn v, if v == nil then r.reason else nil\n\tend, report)\nend\n\n")
	}
	return nil
}
