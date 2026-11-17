package main

func init() {
	// Removes the compressed-float range refusal from the Elixir packet
	// writer (SPEC §5.1 row 10), leaving the quantizer's own non_finite_float
	// check in place, so only the off-range write cases may go red.
	sabotages["packet-write-cfloat-elixir-range"] = []edit{{
		old: `	what := g.dsp(name)
	minLit := f32lit(float64(float32(f.FMin)))
	maxLit := f32lit(float64(float32(f.FMax)))
	g.raiseIf(fmt.Sprintf("is_number(%s) and %s < %s", name, name, minLit),
		fmt.Sprintf("%s is below the compressed float minimum", what), ind)
	g.raiseIf(fmt.Sprintf("is_number(%s) and %s > %s", name, name, maxLit),
		fmt.Sprintf("%s is above the compressed float maximum", what), ind)
`,
		new: `	// SABOTAGED: no compressed-float range refusal
`,
	}}
}
