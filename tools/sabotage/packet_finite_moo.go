package main

// SPEC §4.3, the moo leg: a bare float is finite. Each sabotage removes one
// direction at every float slot. The write one drops the generated finite
// guards (float32, its compressed form, float64), so a non-finite pattern
// reaches the runtime, which latches or traps instead of the unlatched false;
// the read one reads the float bits raw instead of through the runtime's
// refusing float calls. The other direction, the finite reads and -0 and the
// subnormals stay green: the control's own witness that only one was taken.
func init() {
	sabotages["packet-finite-moo-write"] = []edit{
		{
			old: `g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, 0xFFFFFFFF) or bit32.band(%s, 0x7F800000) == 0x7F800000", val, val),`,
			new: `g.refuse(ind, fmt.Sprintf("not isInt(%s, 0, 0xFFFFFFFF) --[[ SABOTAGED: bit32.band(%s, 0x7F800000) == 0x7F800000 ]]", val, val),`,
		},
		{
			old: `g.refuse(ind, fmt.Sprintf("not serialize.Uint64.is(%s) or bit32.band(%s[2], 0x7FF00000) == 0x7FF00000", val, val),`,
			new: `g.refuse(ind, fmt.Sprintf("not serialize.Uint64.is(%s) --[[ SABOTAGED: bit32.band(%s[2], 0x7FF00000) == 0x7FF00000 ]]", val, val),`,
		},
	}
	sabotages["packet-finite-moo-read"] = []edit{
		{
			old: `g.call(ind, "stream:serializeFloatBits(NUM)", "refuses NaN and the infinities (SPEC §4.3)")`,
			new: `g.call(ind, "stream:serializeBits(NUM, 32)", "SABOTAGED: NaN and the infinities ride")`,
		},
		{
			old: `g.call(ind, "stream:serializeDoubleBits(WIDE)", "refuses NaN and the infinities (SPEC §4.3)")`,
			new: `g.call(ind, "stream:serializeBits64(WIDE, 64)", "SABOTAGED: NaN and the infinities ride")`,
		},
	}
}
