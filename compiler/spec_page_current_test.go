// TestSpecPageCurrent holds the sentences this card corrects in docs/SPEC.md
// to the standard and the tree: §6.1's flat JavaScript tier holds its write
// checks in every build (docs/rulings.json `write-refusal`; the per-leg table
// in §5.1), §4.3's backend versions are the sibling pins the Makefile stages
// (SERIALIZE_C_PIN, SERIALIZE_JS_PIN and SERIALIZE_CS_PIN), and §5.1's row 10
// names `invalid_value` as the recommended reason, never as the cross-language
// answer (docs/rulings.json `refusal-verdict-only`). Each corrected statement
// is a want; each removed phrase is a stale.
package compiler

import "testing"

func TestSpecPageCurrent(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC.md"
	types := specSection(t, page, "### 4.3 Field types and their wire encodings", "### 4.4 ")
	write := specSection(t, page, "### 5.1 The write contract", "## 6. Generated code")
	generated := specSection(t, page, "### 6.1 What is generated", "### 6.2 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§6.1 the flat tier's writer checks every build",
			text: generated,
			wants: []string{
				"The flat tier's writer holds its",
				"checks in every build, whatever `NODE_ENV` says",
				"it reads no `NODE_ENV`",
				"and selects no production variant",
			},
			stales: []string{
				"That tier owns the fork itself",
				"`NODE_ENV` is read ONCE at module load",
				"tree-shakes the checked writers",
				"a writer with no contract to check is one function for both builds",
			},
		},
		{
			row:  "§4.3 the backend versions are the Makefile's sibling pins",
			text: types,
			wants: []string{
				"C (serialize.c v2.1.1)",
				"JavaScript (serialize.js v2.1.2)",
				"serialize.cs v2.1.1",
			},
			stales: []string{
				"C (serialize.c v2.0.2)",
				"JavaScript (serialize.js v2.0.1)",
				"serialize.cs v2.0.1",
			},
		},
		{
			row:  "§5.1 row 10 names the recommended reason",
			text: write,
			wants: []string{
				"refused (recommended name `invalid_value` on write; `out_of_range` on read)",
			},
			stales: []string{
				"refused (`invalid_value` on write; `out_of_range` on read)",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.row, r.text, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.row, r.text, stale)
		}
	}
}
