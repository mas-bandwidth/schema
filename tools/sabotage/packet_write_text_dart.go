package main

func init() {
	sabotages["packet-write-text-dart-write"] = []edit{{
		old: "\tg.assertRange(length, big.NewInt(0), big.NewInt(f.Type.Size), true, ind)\n\tif f.Type.Kind == ir.TString {\n\t\tg.useRuntime()\n",
		new: "\tg.assertRange(length, big.NewInt(0), big.NewInt(f.Type.Size), true, ind)\n\tif false && f.Type.Kind == ir.TString { // SABOTAGED: write UTF-8 and null checks removed\n\t\tg.useRuntime()\n",
	}}
}
