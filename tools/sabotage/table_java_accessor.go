package main

func init() {
	// docs/PORTING.md J1: a block or cook SCALAR accessor reads four bytes off
	// its own offset, so the accessor and the descriptor disagree about the value.
	sabotages["java-agree-accessor-scalar"] = []edit{{
		old: "		g.f(\"    public static %s %s(byte[] data, int at) { return %s; }\\n\\n\",\n\t\t\ttyp, name, readCall(typ, fmt.Sprintf(\"at + %d\", pieces[0].Offset), g.fieldName(f)))",
		new: "		g.f(\"    public static %s %s(byte[] data, int at) { return %s; }\\n\\n\",\n\t\t\ttyp, name, readCall(typ, fmt.Sprintf(\"at + %d + 4 /* SABOTAGED */\", pieces[0].Offset), g.fieldName(f)))",
	}}

	// docs/PORTING.md J1: a POINTER SLOT's own offset moves eight bytes — the
	// position a self-relative delta is relative to (§6.3) — so the slot accessor
	// and the descriptor disagree about where the slot lies.
	sabotages["java-agree-slot-offset"] = []edit{{
		old: "		g.f(\"    public static int %sSlot(int at) { return at + %d; }\\n\", name, pieces[0].Offset)",
		new: "		g.f(\"    public static int %sSlot(int at) { return at + %d + 8 /* SABOTAGED */; }\\n\", name, pieces[0].Offset)",
	}}
}
