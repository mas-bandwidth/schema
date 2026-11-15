package main

// The C packet writer's compressed-float value check (SPEC §5.1 row 10),
// removed from the emitter so test/packet-write-cfloat/c/ must red on the
// cases whose values lie outside [min, max].
func init() {
	sabotages["packet-write-cfloat-c-write-range"] = []edit{{
		old: `			g.pf("%sserialize_assert( %s >= %s && %s <= %s );\n", ind, expr,
				formatFloat32(min32), expr, formatFloat32(max32))`,
		new: `			/* SABOTAGED: the compressed-float value check is removed */`,
	}}
}
