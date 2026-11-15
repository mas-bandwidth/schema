// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §2.10,
// §3.4, §21.2, §22.2, §22.6; docs/rulings.json fixed-deprecated-field-is-carried).
//
// The rule, dated 2026-11-10: a deprecated field in the fixed form reads and
// writes like any other field. The reader lands the stored value and checks it
// as it checks any field of its kind; the writer stores what the value holds
// and refuses what any field of its kind would refuse. The declared default
// applies only where the record does not supply the field, an older layout
// read through the lineage. A deprecated ENUM VARIANT (§2.15) is unchanged.
//
// The stale sentences are the ones the ruling retired: §3.4's "THE WRITER
// EMITS THE DECLARED DEFAULT INTO IT AND THE READER YIELDS THE DECLARED
// DEFAULT OUT OF IT, whatever the record carries there", §21.2's "In the FIXED
// FORM, §3.4 takes precedence", §22.2's "The fixed form is the other way", and
// §22.6's "A | deprecated SLOT CARRIES ITS DEFAULT IMAGE, FOR EVERY SHAPE".
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC-TABLES.md"
	slot := specSection(t, page, "### 3.4 ", "## 4. Versioning, and the one read")
	newer := specSection(t, page, "### 21.2 ", "### 21.3 ")
	body := specSection(t, page, "### 22.2 ", "#### 22.2.1 ")
	forms := specSection(t, page, "### 22.6 ", "### 22.7 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§3.4: the slot stays and the field is carried",
			text: slot,
			wants: []string{
				"A DEPRECATED FIELD KEEPS ITS SLOT FOREVER",
				"THE WRITER STORES WHAT THE VALUE HOLDS AND THE READER LANDS THE STORED VALUE",
				"the reader checks it as it checks any field of its kind",
				"the writer refuses what any field of its kind would refuse",
				"The declared default applies only where the record does not supply the field",
			},
			stales: []string{
				"THE WRITER EMITS THE DECLARED DEFAULT INTO IT",
				"THE READER YIELDS THE DECLARED DEFAULT OUT OF IT",
				"whatever the record carries there",
			},
		},
		{
			row:  "§21.2: a newer reader carries a deprecated field in both forms",
			text: newer,
			wants: []string{
				"A field the reader deprecated is read into its slot by every plan",
				"ignored by the application",
				"no counter moves for it",
			},
			stales: []string{
				"In the FIXED FORM, §3.4 takes precedence",
				"the writer emits the deprecated field’s declared default",
				"whatever the record carries",
			},
		},
		{
			row:  "§22.2: the fixed form writes a deprecated field as held too",
			text: body,
			wants: []string{
				"A `| deprecated` FIELD IS WRITTEN AS HELD on this form",
				"The fixed form writes it as held too",
			},
			stales: []string{
				"The fixed form is the other way",
			},
		},
		{
			row:  "§22.6: a deprecated slot is carried in the fixed form",
			text: forms,
			wants: []string{
				"A `| deprecated` SLOT IS WRITTEN AS HELD, LIKE ANY FIELD",
				"the writer stores what the value holds",
				"the reader lands the stored value",
				"The declared default applies only where the record does not supply the field",
			},
			stales: []string{
				"SLOT CARRIES ITS DEFAULT IMAGE",
				"Go copies the owner's default body",
				"the C++ writer writes the field from the owner's reset value",
			},
		},
	}

	// the page wraps at a fixed width, so a sentence is matched with its line
	// breaks folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, r := range rows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			text := flat(r.text)
			for _, want := range r.wants {
				if !strings.Contains(text, flat(want)) {
					t.Errorf("%s: the section no longer states %q", r.row, want)
				}
			}
			for _, stale := range r.stales {
				if strings.Contains(text, flat(stale)) {
					t.Errorf("%s: the section still states the retired sentence %q", r.row, stale)
				}
			}
		})
	}

	t.Run("docs/rulings.json records the ruling", func(t *testing.T) {
		t.Parallel()
		r, ok := readRulings(t)["fixed-deprecated-field-is-carried"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling \"fixed-deprecated-field-is-carried\"")
		}
		if r.Date != "2026-11-10" {
			t.Errorf("the ruling's date is %q, want 2026-11-10", r.Date)
		}
		const quote = "if it's supplied, use the value there, otherwise, use the default."
		quoted := false
		for _, q := range r.Quotes {
			if q == quote {
				quoted = true
			}
		}
		if !quoted {
			t.Errorf("the ruling does not quote %q", quote)
		}
		const sentence = "ruled 2026-11-10: a deprecated field in the fixed form is carried; the declared default applies only where the record does not supply the field"
		if !strings.Contains(r.Source, sentence) {
			t.Errorf("the ruling does not state %q", sentence)
		}
	})
}
