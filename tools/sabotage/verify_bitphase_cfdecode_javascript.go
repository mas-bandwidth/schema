package main

func init() {
	// Puts back the defect the compressed-float decode case was written for
	// (SPEC §4.6, "The decode is normative"): the flat tier's folded literals
	// spelled as the float32's shortest decimal, which JavaScript parses to a
	// different double, so the decode adds min = -3.3 and not float32(-3.3).
	// verify-bitphase's CfDecode reads 2,535 of its 11,001 indices one bit off
	// on the flat tier; the stream tier, whose runtime frounds its parameters,
	// stays exact.
	sabotages["verify-bitphase-cfdecode-js-flat"] = []edit{{
		old: `	s := strconv.FormatFloat(float64(float32(v)), 'g', -1, 64)`,
		new: `	s := strconv.FormatFloat(float64(float32(v)), 'g', -1, 32) // SABOTAGED: the float32's shortest decimal, read as a double`,
	}}
}
