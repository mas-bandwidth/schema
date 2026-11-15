package main

func init() {
	// Removes the packet writer's width check for a bare integer (SPEC §5.1),
	// one tier at a time: the flat tier's guard and the runtime tier's
	// refusal before the runtime's bit call. The mask then wraps an
	// out-of-width value instead of refusing it.
	sabotages["packet-write-bareint-js-flat-width"] = []edit{{
		old: "func (g *fgen) guardBareInt(f *ir.Field, name, ind string) {\n",
		new: "func (g *fgen) guardBareInt(f *ir.Field, name, ind string) {\n\tif f != nil {\n\t\treturn // SABOTAGED: no bare-integer width check\n\t}\n",
	}}
	sabotages["packet-write-bareint-js-runtime-width"] = []edit{{
		old: "func (g *gen) emitBareIntCheck(f *ir.Field, name, ind string) {\n",
		new: "func (g *gen) emitBareIntCheck(f *ir.Field, name, ind string) {\n\tif f != nil {\n\t\treturn // SABOTAGED: no bare-integer width check\n\t}\n",
	}}
}
