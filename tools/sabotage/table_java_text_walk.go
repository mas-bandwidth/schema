// THE JAVA LEG'S TEXT-READ WALK CONTROL (docs/PORTING.md I11): the generated
// fromJson reads an object by matching each key against the field names. Take
// the match away, and every key falls through to the unknown branch, so the
// reader populates nothing and the JSON round-trip fails — while the writer,
// which never consults the key match, keeps writing valid JSON and stays green.
package main

func init() {
	sabotages["java-text-walk-field-match"] = []edit{
		{
			old: "		fmt.Fprintf(b, \"            if (%q.equals(key)) {\\n\", jsonKey(f))",
			new: "		fmt.Fprintf(b, \"            if (false && %q.equals(key)) { // SABOTAGED: no field key ever matches\\n\", jsonKey(f))",
		},
	}
}
