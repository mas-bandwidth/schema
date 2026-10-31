package main

// The Goose packet wire's controls (make/goose-packet.mk). Each removes one
// rule from the emitter and must fail exactly the rows that rule holds.
func init() {
	// SPEC §4.7: the read's UTF-8 validation; the harness's mutation sweep
	// must then disagree with the C++ reader.
	sabotages["packet-utf8-goose-read"] = []edit{{
		old: `w.Pf("%sguard serialize_valid_utf8(%s[0..%s]) else {`,
		new: `/* SABOTAGED */ w.Pf("%sguard true || serialize_valid_utf8(%s[0..%s]) else {`,
	}}
	// SPEC §4.12: the read's surrogate pairing.
	sabotages["packet-wide-goose-pairing"] = []edit{{
		old: `w.Pf("%s    guard low%s == %s else { %s } // an unpaired surrogate (SPEC §4.12)\n", ind, g, el, failRange)`,
		new: `w.Pf("%s    guard true || low%s == %s else { %s } // SABOTAGED\n", ind, g, el, failRange)`,
	}}
	// SPEC §4.2: the constructor's byte default, its length kept.
	sabotages["packet-defaults-goose-constructor-bytes"] = []edit{{
		old: `out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d]", unit, size), Init: init, Comment:`,
		new: `_ = init; out = append(out, Slot{Name: name, Type: fmt.Sprintf("%s[%d]", unit, size), Init: "" /* SABOTAGED */, Comment:`,
	}}
	// SPEC §4.3, the read direction: the bits ride into the field with no
	// finite test, so only the crafted-bytes read vectors may go red.
	sabotages["packet-finite-goose-read"] = []edit{
		{
			old: `w.Pf("%sguard serialize_float(s, %s) else { return false; }\n", ind, expr)`,
			new: `w.Pf("%s{ var sab: u32 = 0; guard serialize_bits(s, sab, 32) else { return false; } %s = serialize_f32_from_bits(sab); } // SABOTAGED\n", ind, expr)`,
		},
		{
			old: `w.Pf("%sguard serialize_double(s, %s) else { return false; }\n", ind, expr)`,
			new: `w.Pf("%s{ var sab: u64 = 0; guard serialize_bits64(s, sab, 64) else { return false; } %s = serialize_f64_from_bits(sab); } // SABOTAGED\n", ind, expr)`,
		},
	}
	// SPEC §4.3, the write direction: the finite refusals go, so only the
	// write vectors may go red.
	sabotages["packet-finite-goose-write"] = []edit{
		{
			old: `w.Pf("%sif !serialize_float_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; }`,
			new: `/* SABOTAGED */ w.Pf("%sif false && !serialize_float_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; }`,
		},
		{
			old: `w.Pf("%sif !serialize_double_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; }`,
			new: `/* SABOTAGED */ w.Pf("%sif false && !serialize_double_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; }`,
		},
	}
}
