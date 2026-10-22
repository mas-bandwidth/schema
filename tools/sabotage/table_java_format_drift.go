// THE JAVA FORMAT GATE'S SABOTAGE (docs/JAVA-DIALECT.md, issue #547), and it
// moves WHITESPACE rather than a byte of meaning.
//
// tables-java-clean holds every generated Java file to what the pinned
// google-java-format writes. A formatter gate that has never been watched fail
// could be handed no files, the wrong files, or a formatter that answers yes to
// everything, so this writes ONE line of the constant TableBytes.java source
// one space deeper than the style -- the smallest drift a formatter can see and
// javac cannot -- and make tables-java-clean-drift-negative-control requires the
// gate to go RED naming that file.
package main

func init() {
	sabotages["table-java-format-drift"] = []edit{{
		old: "public final class TableBytes {\n    private TableBytes() {}\n",
		new: "public final class TableBytes {\n     private TableBytes() {}\n` + /* SABOTAGED: one line mis-indented by a space */ `",
	}}
}
