package main

func init() {
	sabotages["packet-write-text-java-check"] = []edit{{
		old: `	case ir.TString:
		g.assertRange(name+"Length", big.NewInt(0), big.NewInt(f.Type.Size), true, ind)
		g.emitUTF8(name, name+"Length", ind, "return false")
		iv := fmt.Sprintf("i%d", g.loopDepth)
		g.loopDepth++
		g.pf("%sfor (int %s = 0; %s < %sLength; %s++) {\n", ind, iv, iv, name, iv)
		g.pf("%s    if (%s[%s] == 0) {\n", ind, name, iv)
		g.pf("%s        return false; // an interior null is content the write refuses (SPEC §4.7)\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
		g.loopDepth--`,
		new: `	case ir.TString: // SABOTAGED: drop the write text content contract
		g.assertRange(name+"Length", big.NewInt(0), big.NewInt(f.Type.Size), true, ind)`,
	}}
}
