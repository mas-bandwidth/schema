package main

func init() {
	// Removes SPEC §5.1 row 1's bare-integer write check from the Elixir
	// emitter's packet writer: a non-integer or a value outside the declared
	// intN/uintN width is masked onto the wire instead of refused.
	sabotages["packet-write-bareint-elixir"] = []edit{{
		old: `	g.raiseIf(fmt.Sprintf("not is_integer(%s)", name), fmt.Sprintf("%s is not an integer", g.dsp(name)), ind)
	g.emitCheckRange(name, g.dsp(name), lo, hi, ind)
	g.emitWriteWideRaw(name, w, ind)`,
		new: `	_, _ = lo, hi // SABOTAGED: no bare-integer write check
	g.emitWriteWideRaw(name, w, ind)`,
	}}
}
