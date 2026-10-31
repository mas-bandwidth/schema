package main

// The JavaScript table leg's negative controls for the porting register's
// 3.0.0 correctness rows (docs/PORTING.md M5, M6, M11, M12, M14, M18, M21, I6,
// I7, I12, J3). Each breaks the EMITTER (internal/codegen/jstable) so the code
// the named gate runs is wrong, never the gate itself; make/js.mk builds the
// compiler or the jstable tests against the copy through a Go overlay.
func init() {
	// M5: the read accessor of a variable- or fixed-class keyed array loses
	// its shift, so key k reads slot k. The guards are untouched, so the
	// refusal at both ends still holds and only the placement is wrong.
	sabotages["js-keyed-no-shift"] = []edit{{
		old: `return value.%s[key - 1];\n  }\n", prop, guard, prop)`,
		new: `return value.%s[key];\n  }\n", prop, guard, prop) // SABOTAGED: key k reads slot k`,
	}}

	// M6: the numbering walk forgets a CLOSED node, so a node two pointers
	// name is numbered and written twice. The open set still refuses a cycle,
	// so the walk terminates and only identity is gone.
	sabotages["js-identity-dropped"] = []edit{{
		old: "if (nodes.index.has(e.val)) { continue; }",
		new: "/* SABOTAGED: a closed node is a first visit again */",
	}}

	// M14: the text writer never finds a label it already wrote, so a shared
	// node is DEFINED with `&node` at every occurrence instead of referenced.
	sabotages["js-label-dropped"] = []edit{{
		old: "const label = g.labels.get(node);",
		new: "const label = undefined; // SABOTAGED: every occurrence defines the node",
	}}

	// M18: an arm's L is the field line's L. Writing it one long frames the
	// arm body wrong on the wire.
	sabotages["js-arm-length"] = []edit{{
		old: "  if (!TableVariableArmPayload(arm, body, ids, nodes, val, retain, path, o)) { return false; }\n  w.leb(body.len);",
		new: "  if (!TableVariableArmPayload(arm, body, ids, nodes, val, retain, path, o)) { return false; }\n  w.leb(body.len + 1); // SABOTAGED: the arm's L is one long",
	}}

	// M21: elision compares a float by VALUE rather than by bits, so a -0
	// under a +0 default is elided and reads back +0.
	sabotages["js-float-value-compare"] = []edit{{
		old: "return f32 ? Object.is(Math.fround(a), Math.fround(b)) : Object.is(a, b);",
		new: "return f32 ? Math.fround(a) === Math.fround(b) : a === b; // SABOTAGED: compare by value",
	}}

	// I6: the fixed runtime binds one more module-scope name that nothing
	// claims, so a schema declaring it would collide with the generated module.
	sabotages["js-names-planted"] = []edit{{
		old: "export const TableFixedForm = 3;",
		new: "export const BogusUnregistered = 0; // SABOTAGED: an unclaimed module-scope name\nexport const TableFixedForm = 3;",
	}}

	// M11: one array's generated PITCH constant in the layout check moves off
	// the row object's size, so the build's two derivations disagree.
	sabotages["js-block-pitch"] = []edit{{
		old: `g.pf("    { What: %q, Stride: %d, SizeOf: () => %s.Size },\n",`,
		new: `g.pf("    { What: %q, Stride: %d + 8, SizeOf: () => %s.Size }, // SABOTAGED: the pitch constant moved\n",`,
	}}

	// M12: two accessor reads drop the explicit little-endian flag, so they
	// read in an order that is not the reader's.
	sabotages["js-byte-order"] = []edit{
		{old: `return fmt.Sprintf("view.getUint32(%s, true)", at)`, new: `return fmt.Sprintf("view.getUint32(%s)", at) // SABOTAGED: big-endian`},
		{old: `return fmt.Sprintf("view.getInt32(%s, true)", at)`, new: `return fmt.Sprintf("view.getInt32(%s)", at) // SABOTAGED: big-endian`},
	}

	// I7: the block's Open stops reading the prologue's order word, so a block
	// whose magic is intact and whose order word says the other order opens.
	sabotages["js-order-word"] = []edit{{
		old: `g.pf("    if (view.getBigUint64(16, true) !== TableBlockByteOrder) { return \"not_a_cook\"; }\n")`,
		new: `g.pf("    // SABOTAGED: the order word is not read\n")`,
	}}

	// I12: the cook's documented entry point moves, so docs/USAGE.md's
	// example no longer runs against what the emitter writes.
	sabotages["js-cook-open-moved"] = []edit{{
		old: `g.pf("    Open, OpenReason, At, Layout, Type: root,\n")`,
		new: `g.pf("    Opened: Open, OpenReason, At, Layout, Type: root, // SABOTAGED: Open moved\n")`,
	}}

	// J3: the block reader's two extent bounds come out (the fuzz oracle's
	// own control, tables-js-fuzz-negative-control), so the release tier's
	// fuzzer at depth must find a read that leaves the buffer.
	sabotages["js-block-extent-bounds"] = []edit{
		{old: `g.pf("      if (rows > extent - offsetOf) { return \"bad_layout\"; }\n")`, new: `// SABOTAGED: no row bound`},
		{old: `g.pf("    if (padding > extent - used) { return \"truncated\"; }\n")`, new: `// SABOTAGED: no padding bound`},
	}
}
