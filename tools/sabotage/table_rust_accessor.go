package main

// The Rust accessor/descriptor agreement controls (docs/PORTING.md J1).
//
// Both break internal/codegen/rusttable/cook.go, which writes the #[repr(C)]
// row structs (the ACCESSOR side: the compiler places each member) and the
// TableCookInfo descriptors (the DESCRIPTOR side: literal offsets from the
// layout model).
func init() {
	// The ACCESSOR control swaps the declaration order of RenderCamera's two
	// adjacent u32 members camera_id and camera_type, so each accessor reads where
	// the other one is described. The sizes and the alignment do not change, so
	// the struct's size and alignment asserts hold, and the control drops the
	// per-member offset_of! asserts for the pair: they are the COMPILE-TIME layer
	// that would otherwise stop the build before the agreement leg ran at all.
	sabotages["rust-accessor-moved"] = []edit{
		{
			old: "g.emitCookRowField(f)",
			new: "g.emitCookRowField(accessorMovedField(st, f)) // SABOTAGED: camera_id and camera_type swap places",
		},
		{
			old: "func cookRowMembers(f *ir.Field) []string {",
			new: `func accessorMovedField(st *ir.Struct, f *ir.Field) *ir.Field {
	if st.Name != "RenderCamera" || (f.Name != "camera_id" && f.Name != "camera_type") {
		return f
	}
	for _, o := range st.Fields {
		if o != f && (o.Name == "camera_id" || o.Name == "camera_type") {
			return o
		}
	}
	return f
}

func cookRowMembers(f *ir.Field) []string {`,
		},
		{
			old: "			for i, name := range cookRowMembers(fl.Field) {\n",
			new: "			for i, name := range cookRowMembers(fl.Field) {\n				if st.Name == \"RenderCamera\" && (name == \"camera_id\" || name == \"camera_type\") {\n					continue // SABOTAGED: the compile-time offset assert is withdrawn for the pair\n				}\n",
		},
	}
	// The SLOT control describes TreeNode.left one reference slot on (eight
	// bytes), where `right` is, while the row struct still places it where it
	// always did. Nothing in the generated crate asserts a descriptor's
	// offset, so the build stays green and only the agreement can see it.
	sabotages["rust-slot-moved"] = []edit{
		{
			old: "	fmt.Fprintf(&b, \"            offset: %d,\\n\", fl.Offset)\n",
			new: "	fmt.Fprintf(&b, \"            offset: %d,\\n\", fl.Offset+slotMoved(f.Name)) // SABOTAGED: a pointer slot described eight bytes on\n",
		},
		{
			old: "func cookRowMembers(f *ir.Field) []string {",
			new: `func slotMoved(name string) int64 {
	if name == "left" {
		return 8
	}
	return 0
}

func cookRowMembers(f *ir.Field) []string {`,
		},
	}
}
