// The C format gate's planted fault (make/c.mk tables-c-format-negative-control,
// docs/PORTING.md J4), in the PACKET emitter, whose output the gate holds to
// zero drift:
//
//   - c-clean-format-drift writes the opening brace of a struct typedef on the
//     line of its name (the K&R form) where the house style, and so
//     clang-format, writes it on its own line.
package main

func init() {
	sabotages["c-clean-format-drift"] = []edit{{
		old: "\tg.pf(\"typedef struct %s\\n{\\n\", d.Name)\n\tfor _, f := range d.Fields {",
		new: "\tg.pf(\"typedef struct %s {\\n\", d.Name) // SABOTAGED: K&R brace\n\tfor _, f := range d.Fields {",
	}}
}
