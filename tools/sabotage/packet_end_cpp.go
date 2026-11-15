package main

// SPEC §5: the generated public Read<T> ends with the runtime's end check.
// Remove that call and a packet with a whole trailing byte or nonzero final
// padding reads as the object alone.
func init() {
	sabotages["packet-end-cpp-read"] = []edit{
		{
			old: `g.pf("    serialize::ReadStream end = stream; // check a copy, so the caller keeps the object's bit count\n")`,
			new: `g.pf("    (void) stream; // SABOTAGED: the packet end check removed\n")`,
		},
		{
			old: `g.pf("    serialize_end( end ); // the packet ends here: no whole byte and no nonzero final padding (SPEC §5)\n")`,
			new: `g.pf("    // SABOTAGED: the packet end check removed\n")`,
		},
	}
}
