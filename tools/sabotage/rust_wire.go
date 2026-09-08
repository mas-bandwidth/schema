package main

// Removing canonical spelling must change the differential read report. Keep
// this control beside the port without touching another language's anchors.
func init() {
	sabotages["rust-wire-nonminimal"] = []edit{{
		old: "if i > 0 && b == 0 {\n                    break;\n                }",
		new: "if false { break; } // SABOTAGED: accept a nonminimal LEB128",
	}}
}
