package main

// the moo packet writer's bool, written twice (docs/PORT-MOO.md, THE GATES):
// TestMooTypeWireWritesTheGoldens must red exactly the C++-pinned goldens
// whose instances write a bool, and every other golden must still match.
func init() {
	sabotages["packet-wire-moo-bool-twice"] = []edit{{
		old: `		g.pf("%sBOOL.value = %s\n", ind, val)
		g.call(ind, "stream:serializeBool(BOOL)", "")`,
		new: `		g.pf("%sBOOL.value = %s\n", ind, val)
		g.call(ind, "stream:serializeBool(BOOL)", "")
		g.call(ind, "stream:serializeBool(BOOL)", "") // SABOTAGED: the bool rides twice`,
	}}
}
