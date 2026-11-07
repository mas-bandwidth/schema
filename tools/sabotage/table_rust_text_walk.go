// THE RUST LEG'S TEXT-READ WALK CONTROL (docs/PORTING.md I11): the generated
// <name>_json_fields matches each key of an object against the field names.
// Guard every field arm with `if false`, and every key falls through to the
// unknown branch, so the reader populates nothing and the JSON read fails —
// while the writer and the wire codec, which never consult the key match,
// stay green.
package main

func init() {
	sabotages["rust-text-walk-field-match"] = []edit{
		{
			old: "	fmt.Fprintf(b, \"%s%q => {\\n\", ind, g.fieldKey(f))",
			new: "	fmt.Fprintf(b, \"%s%q if false => { // SABOTAGED: no field key ever matches\\n\", ind, g.fieldKey(f))",
		},
	}
}
