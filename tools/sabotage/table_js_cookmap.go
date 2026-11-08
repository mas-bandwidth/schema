package main

// The JavaScript cook of maps and lists (docs/PORTING.md M19): make/js.mk's
// tables-js-cook-map controls, each against internal/codegen/jstable/cookwrite.go.
func init() {
	// the carve lays a map's entries in the Map's own insertion order: the
	// writer no longer holds the sort
	sabotages["js-cookmap-unsorted"] = []edit{{
		old: `      return TableVariableMapEntries({ c: layouts[fd.t], ks: fd.ks === 1 }, v);`,
		new: `      return Array.from(v, ([key, value]) => { const e = layouts[fd.t].make(); e.key = key; e.value = value; return e; }); // SABOTAGED: the entries unsorted`,
	}}
	// an array is laid at the running offset, not at its element's alignment
	sabotages["js-cookmap-unaligned"] = []edit{{
		old: `    cur.at = alignUp(cur.at, fd.align);`,
		new: `    // SABOTAGED: the array is laid unaligned`,
	}}
	// the extent begins at sizeof rounded to the region's floor of eight alone,
	// not at ir.CookExtentStart: a sixteen-aligned array lands at eight modulo
	// sixteen
	sabotages["js-cookmap-extent-start"] = []edit{{
		old: `ir.CookNodeAlign(u, st), ir.CookExtentStart(u, st))`,
		new: `ir.CookNodeAlign(u, st), cookAlignUp(ir.RecordLayout(u, st).Size, 8)) // SABOTAGED: the extent starts at the floor`,
	}}
	// the node aligns to its record's alignment alone, not ir.CookNodeAlign: a
	// node whose extent holds a sixteen-aligned array lands at eight
	sabotages["js-cookmap-node-align"] = []edit{{
		old: `ir.CookNodeAlign(u, st), ir.CookExtentStart(u, st))`,
		new: `ir.RecordLayout(u, st).Align, ir.CookExtentStart(u, st)) // SABOTAGED: the node aligns to its record alone`,
	}}
}
