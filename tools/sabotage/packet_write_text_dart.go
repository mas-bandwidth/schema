package main

// SPEC §5.1 row 8: the packet writer holds a string's used bytes to no zero
// byte and well-formed UTF-8. Remove the write content contract from the
// emitter; the read-side check stays, so only the ill-formed writes go red.
func init() {
	sabotages["packet-write-text-dart-write"] = []edit{{
		old: `	if f.Type.Kind == ir.TString {
		g.useRuntime()
		check := fmt.Sprintf("%sassert(Schema.utf8Valid(%s, %s));", ind, name, length)
		if len(check) <= 80 {
			g.pf("%s\n", check)
		} else {
			g.pf("%sassert(\n%s  Schema.utf8Valid(\n%s    %s,\n%s    %s,\n%s  ),\n%s);\n",
				ind, ind, ind, name, ind, length, ind, ind)
		}
		g.pf("%s// an interior null is content the write refuses (SPEC §4.7)\n", ind)
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		hi := g.scratchHi
		g.emitByteReadLoop(ind, iv, length)
		g.scratchHi = hi
		one := fmt.Sprintf("%s  assert(%s[%s] != 0);", ind, name, iv)
		if len(one) <= 80 {
			g.pf("%s\n", one)
		} else {
			g.pf("%s  assert(\n%s    %s[%s] != 0,\n%s  );\n", ind, ind, name, iv, ind)
		}
		g.pf("%s}\n", ind)
		g.loopDepth--
	}
`,
		new: `	// SABOTAGED: drop the write text content contract
`,
	}}
}
