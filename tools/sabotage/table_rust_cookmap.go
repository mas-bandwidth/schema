package main

// The Rust cook of maps and lists (docs/PORTING.md M19): test/rust-cookmap's
// three negative controls, each against internal/codegen/rusttable/cookcarve.go.
func init() {
	sabotages["rust-cookmap-unsorted"] = []edit{{
		old: `"%sorder.sort_by(|&a, &b| %s);\n"`,
		new: `"%slet _ = |a: usize, b: usize| %s; order.reverse(); // SABOTAGED: the entries unsorted\n"`,
	}}
	sabotages["rust-cookmap-wide-extent"] = []edit{{
		old: `	return ir.CookExtentStart(u, st)`,
		new: `	return (ir.RecordLayout(u, st).Size + ir.RegionAlignFloor - 1) / ir.RegionAlignFloor * ir.RegionAlignFloor // SABOTAGED: the extent rounded to the floor alone`,
	}}
	sabotages["rust-cookmap-unaligned"] = []edit{{
		old: `        self.at = (self.at + align - 1) / align * align;`,
		new: `        let _ = align; // SABOTAGED: the array is laid unaligned`,
	}}
}
