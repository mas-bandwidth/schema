package main

func init() {
	// Removes the packet writer's [min, max] value check for a compressed
	// float (SPEC §5.1 row 10), one tier at a time: the flat tier's guard on
	// the float32 value, and the runtime tier's guard before the runtime's
	// compressed-float call. The finite check stays, so only a finite value
	// off its range may reach the quantizer.
	sabotages["packet-write-cfloat-js-flat-range"] = []edit{{
		old: `	g.guard(fmt.Sprintf("x < %s || x > %s", f32lit(float64(minF)), f32lit(float64(maxF))), "", ind)`,
		new: `	// SABOTAGED: no [min, max] write check`,
	}}
	sabotages["packet-write-cfloat-js-runtime-range"] = []edit{{
		old: `			g.pf("%s  if (!Number.isFinite(x) || x < %s || x > %s) {\n",
				ind, formatFloat32(f.FMin), formatFloat32(f.FMax))
			g.pf("%s    return false; // a compressed float off its range is refused before quantizing (SPEC §5.1)\n", ind)
			g.pf("%s  }\n%s}\n", ind, ind)`,
		new: `			g.pf("%s  void x; // SABOTAGED: no [min, max] write check\n%s}\n", ind, ind)`,
	}}
}
