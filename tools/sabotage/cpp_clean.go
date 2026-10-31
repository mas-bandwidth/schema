// THE C++ CLEAN GATE'S SABOTAGES (docs/PORTING.md J4, make/cpp.mk
// tables-cpp-clean). Each breaks the PACKET emitter, whose output is held to
// zero (a format drift and a warning are both findings there, with no ratchet
// entry to hide behind), by the smallest change that cannot alter what the
// generated code means.
//
//   - cpp-clean-format-drift writes the opening brace of a struct on the line
//     of its name (the K&R form) where the house style, and so clang-format,
//     writes it on its own line.
//   - cpp-clean-old-style-cast writes a union arm's placement-new with the
//     C-style cast the emitter replaced with a static_cast: the same
//     instruction, and exactly the -Wold-style-cast the warnings gate must name.
package main

func init() {
	sabotages["cpp-clean-format-drift"] = []edit{{
		old: `g.pf("// type %s\n", d.Name)` + "\n\t" + `g.pf("struct %s\n{\n", d.Name)`,
		new: `g.pf("// type %s\n", d.Name)` + "\n\t" + `g.pf("struct %s {\n", d.Name) // SABOTAGED: K&R brace`,
	}}
	sabotages["cpp-clean-old-style-cast"] = []edit{{
		old: `::new ( static_cast<void *>( &%s ) ) %s{};`,
		new: `::new ( (void *) &%s ) %s{}; // SABOTAGED: a C-style cast`,
	}}
}
