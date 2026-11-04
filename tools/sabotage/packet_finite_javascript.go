package main

// The JavaScript flat tier's finite float rule (SPEC §4.3): its writer refuses
// a NaN or an infinity in a plain float with -1, in every build (docs/SPEC.md
// §5.1). packet-finite-js-flat-write removes the two refusals, so exactly the
// write vectors go red while the finite round trips stay green.
// packet-finite-js-flat-production puts a NODE_ENV fork back into every
// writer refusal, so the leg's NODE_ENV=production run goes red on the same
// vectors while its development run stays green.
func init() {
	sabotages["packet-finite-js-flat-write"] = []edit{
		{
			old: `		g.guard(fmt.Sprintf("!Number.isFinite(%s)", name), " // a NaN or an infinity is refused (SPEC §4.3)", ind)` + "\n",
			new: "\t\t// SABOTAGED: no float64 finite check\n",
		},
		{
			old: `	g.guard("!Number.isFinite(x)", " // a NaN or an infinity is refused (SPEC §4.3)", ind)` + "\n",
			new: "\t// SABOTAGED: no float32 finite check\n",
		},
	}
	sabotages["packet-finite-js-flat-production"] = []edit{{
		old: `	g.pf("%sif (%s) {%s\n%s  return -1;\n%s}\n", ind, cond, comment, ind, ind)`,
		new: `	g.pf("%sif (process.env.NODE_ENV !== \"production\" && (%s)) { // SABOTAGED: production writes unchecked%s\n%s  return -1;\n%s}\n", ind, cond, comment, ind, ind)`,
	}}
}
