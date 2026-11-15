// The packet-write compressed-float range gate's planted fault (make/cs.mk
// packet-write-cfloat-cs-negative-control, docs/SPEC.md §5.1 row 10): the
// generated packet writer's finite/range assertion becomes a constant true, so
// the value reaches the runtime quantizer unchecked in a debug build.
package main

func init() {
	sabotages["packet-write-cfloat-cs-range"] = []edit{{
		old: `"float.IsFinite(compressedValue) && compressedValue >= %s && compressedValue <= %s"`,
		new: `"true /* SABOTAGED: the compressed-float range assertion is removed (%s, %s) */"`,
	}}
}
