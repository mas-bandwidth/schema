package main

// SPEC §4.3: a bare float is finite. The Go flat form places float bits
// itself, so it carries the refusal the runtime's float calls carry. Each
// sabotage removes one direction; the per-field shapes, which reach the
// runtime, stay green and are the control's own witness that only the flat
// form's guard was taken away.
func init() {
	sabotages["packet-finite-go-read"] = []edit{{
		old: `g.pf("%sif %s&%s == %s { // a NaN or infinity pattern is refused on its raw bits\n", ind, src, exponent, exponent)`,
		new: `g.pf("%sif false && %s&%s == %s { // SABOTAGED: a NaN or infinity pattern rides\n", ind, src, exponent, exponent)`,
	}}
	sabotages["packet-finite-go-write"] = []edit{{
		old: `g.pf("%sif %s&%s == %s { // NaN and the infinities are not values of the type\n", ind, bitsExpr, exponent, exponent)`,
		new: `g.pf("%sif false && %s&%s == %s { // SABOTAGED: NaN and the infinities ride\n", ind, bitsExpr, exponent, exponent)`,
	}}
}
