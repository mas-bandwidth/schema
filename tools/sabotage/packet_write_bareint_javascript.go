package main

func init() {
	// Removes the packet writer's width check for a bare integer (SPEC §5.1),
	// one tier at a time: the flat tier's guard and the runtime tier's
	// refusal before the runtime's bit call. The mask then wraps an
	// out-of-width value instead of refusing it. The bigint-type pair keeps
	// the range test and takes out only the wide widths' BigInt type test,
	// so a numeric string compares false against both BigInt bounds and is
	// coerced onto the wire.
	sabotages["packet-write-bareint-js-flat-width"] = []edit{{
		old: "func (g *fgen) guardBareInt(f *ir.Field, name, ind string) {\n",
		new: "func (g *fgen) guardBareInt(f *ir.Field, name, ind string) {\n\tif f != nil {\n\t\treturn // SABOTAGED: no bare-integer width check\n\t}\n",
	}}
	sabotages["packet-write-bareint-js-runtime-width"] = []edit{{
		old: "func (g *gen) emitBareIntCheck(f *ir.Field, name, ind string) {\n",
		new: "func (g *gen) emitBareIntCheck(f *ir.Field, name, ind string) {\n\tif f != nil {\n\t\treturn // SABOTAGED: no bare-integer width check\n\t}\n",
	}}
	sabotages["packet-write-bareint-js-flat-bigint-type"] = []edit{{
		old: "g.guard(fmt.Sprintf(\"typeof %s !== \\\"bigint\\\" || %s < %s || %s > %s\", name, name, bigLit(lo), name, bigLit(hi)),",
		new: "g.guard(fmt.Sprintf(\"%s < %s || %s > %s\", name, bigLit(lo), name, bigLit(hi)), // SABOTAGED: no BigInt type test",
	}}
	sabotages["packet-write-bareint-js-runtime-bigint-type"] = []edit{{
		old: "g.pf(\"%sif (typeof %s !== \\\"bigint\\\" || %s < %s || %s > %s) { // a bare integer must fit its width\\n\",\n\t\t\tind, name, name, bigLit(lo), name, bigLit(hi))",
		new: "g.pf(\"%sif (%s < %s || %s > %s) { // SABOTAGED: no BigInt type test\\n\",\n\t\t\tind, name, bigLit(lo), name, bigLit(hi))",
	}}
}
