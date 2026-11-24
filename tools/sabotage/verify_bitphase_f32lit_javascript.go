package main

func init() {
	// Puts back the defect the float32 literal case was written for (SPEC
	// §4.3, "A FLOAT LITERAL IS A float64 FIRST"): js.go spelling a float32
	// constant, default and stream-tier write guard as the float32's shortest
	// decimal, which JavaScript reads as a different double. verify-bitphase's
	// F32Lit reads F32Const and both defaults off on both tiers (the module's
	// constructor serves both), and the stream tier's guard refuses both
	// float32 ends; the flat tier's guard is flat.go's own and still writes
	// them.
	sabotages["verify-bitphase-f32lit-js"] = []edit{{
		old: `	s := strconv.FormatFloat(float64(float32(v)), 'g', -1, 64)`,
		new: `	s := strconv.FormatFloat(float64(float32(v)), 'g', -1, 32) // SABOTAGED: the float32's shortest decimal, read as a double`,
	}}
}
