// WHAT A READER RETURNS FOR A FIELD THE RECORD DOES NOT SUPPLY
// (docs/SPEC-TABLES.md §4.4, docs/rulings.json supplied-or-default, dated
// 2026-11-10).
//
// One normative section states the rule and every other section applies it:
// a value the record supplies is read as stored; a field it does not supply
// (an absent optional's payload, a field an older layout lacks, a field left
// out) reads as its declared default, in every form and every leg, a
// required field excepted; a union arm the tag does not name is not a value;
// a deprecated field is supplied like any other. The test pins the
// section's sentences and holds the retired sentences out of every page that
// once said otherwise.
package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSpecSuppliedOrDefault(t *testing.T) {
	t.Parallel()
	const tables = "../docs/SPEC-TABLES.md"
	const algorithm = "../docs/FIXED-FORM-ALGORITHM.md"
	const wantQuote = "I mean bottom line is, if it's supplied, use the value there, otherwise, use the default."

	rule := specSection(t, tables, "### 4.4 Supplied or default", "## 5. Identity")
	deprecated := specSection(t, tables, "#### The `deprecated` marker", "#### Renaming is not a change")
	record := specSection(t, tables, "#### THE RECORD", "#### SELECTION IS BY THE KEYWORD")
	readerPath := specSection(t, tables, "#### THE ONE READER PATH: A PLAN", "#### THE IN-PLACE IDENTITY READ")
	newer := specSection(t, tables, "### 21.2 What a newer reader", "### 21.3 What an older reader")
	forms := specSection(t, tables, "### 22.6 Forms", "### 22.7 The gate")
	write := specSection(t, algorithm, "### 3.1 The write", "## 4. The reader: ONE path")
	plan := specSection(t, algorithm, "### 4.1 The plan", "### 4.2 Which plan")
	run := specSection(t, algorithm, "### 4.4 The run", "### 4.5 The scatter")
	manifest := specSection(t, algorithm, "**FOUR THINGS THE MANIFEST MUST NOT SAY", "- **A PATH IS A DOTTED SCHEMA PATH")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§4.4: the one rule",
			text: rule,
			wants: []string{
				"A VALUE THE RECORD SUPPLIES IS READ AS STORED",
				"A DEPRECATED FIELD IS SUPPLIED LIKE ANY OTHER",
				"A FIELD THE RECORD DOES NOT SUPPLY READS AS ITS DECLARED DEFAULT",
				"an ABSENT OPTIONAL.",
				"never the bytes the record carried under the flag",
				"A UNION ARM THE TAG DOES NOT NAME.",
				"is not part of the read's result",
				"THE ONE EXCEPTION: a `| required` field",
				"the message form's only dead data is its pad bits",
				"A FIELD THE WRITER'S LAYOUT OR VOCABULARY LACKS",
				"SLOTS PAST A COUNT AND BYTES PAST A LENGTH ARE NOT VALUES.",
				"EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, OR THE READ IS REFUSED.",
				"REFUSES THE WHOLE LOAD as `wire_damaged`",
				"a field rule 2 covers still lands its declared default, not those zeros",
				"WHERE IT APPLIES.",
				"COOKED form",
			},
		},
		{
			row:   "§2.10: a deprecated field still works on both sides",
			text:  deprecated,
			wants: []string{"IT STILL WORKS, ON BOTH SIDES", "The writer stores what the value holds", "The reader lands what the record carries"},
			stales: []string{
				"The writer emits the field's default.",
				"The reader ignores it.",
			},
		},
		{
			row:  "§3.4: absent payload and untaken arm read as declared defaults",
			text: record,
			wants: []string{
				"its payload at `T`'s DECLARED DEFAULTS, never the wire's zeros (§4.4)",
				"an arm the tag does not name is not a value (§4.4)",
				"A DEPRECATED FIELD IS READ AND WRITTEN LIKE ANY OTHER FIELD",
			},
			stales: []string{
				"THE WRITER EMITS THE DECLARED DEFAULT INTO IT AND THE READER YIELDS THE DECLARED DEFAULT OUT OF IT",
				"IGNORED on read",
				"UNSPECIFIED ON READ",
			},
		},
		{
			row:    "§3.4 THE ONE READER PATH: the identity plan lands the declared defaults",
			text:   readerPath,
			wants:  []string{"IT CARRIES FIVE OPS AND NOT ONE", "a `defaults` after each optional's payload"},
			stales: []string{"IT CARRIES FOUR OPS"},
		},
		{
			row:    "§21.2: a deprecated field is read as stored in both forms",
			text:   newer,
			wants:  []string{"§4.4 is the rule here, in both forms.", "the reader lands what the record carries"},
			stales: []string{"§3.4 takes precedence", "reader yields that default whatever the record carries", "nothing reads them"},
		},
		{
			row:    "§22.6: a deprecated field is written like any other",
			text:   forms,
			wants:  []string{"WRITTEN LIKE ANY OTHER FIELD", "a reader refuses a record where one is not zero (§4.4 rule 4)"},
			stales: []string{"CARRIES ITS DEFAULT IMAGE, FOR EVERY SHAPE"},
		},
		{
			row:   "FIXED §3.1: what a read returns",
			text:  write,
			wants: []string{"What a read that is not refused RETURNS is", "never the zeros the record carries there"},
		},
		{
			row:   "FIXED §4.1: the defaults op, and no dead byte lands as a value",
			text:  plan,
			wants: []string{"| `defaults` |", "A DEAD BYTE IS NEVER LANDED AS A VALUE", "`copy`, `count`, `text`, `bool` and `defaults`"},
		},
		{
			row:   "FIXED §4.4: every leg lands the declared defaults",
			text:  run,
			wants: []string{"Every leg lands an absent optional's payload as `T`'s DECLARED DEFAULTS", "every other plan-based leg emits the same order"},
		},
		{
			row:    "FIXED manifest: a reader produces the declared defaults",
			text:   manifest,
			wants:  []string{"what a READER PRODUCES there is the payload's DECLARED DEFAULTS"},
			stales: []string{"the one place \"a field absent from a line carries its schema default\" does not hold"},
		},
	}

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

	t.Run("the ruling quotes the maintainer verbatim", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/rulings.json")
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Rulings []deadBytesRulingEntry `json:"rulings"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		var got *deadBytesRulingEntry
		for i := range file.Rulings {
			if file.Rulings[i].ID == "supplied-or-default" {
				got = &file.Rulings[i]
			}
		}
		if got == nil {
			t.Fatal("docs/rulings.json carries no ruling supplied-or-default")
		}
		if got.Date != "2026-11-10" {
			t.Errorf("date = %q, want 2026-11-10", got.Date)
		}
		if len(got.Quotes) != 1 || got.Quotes[0] != wantQuote {
			t.Errorf("quotes = %q, want exactly %q", got.Quotes, wantQuote)
		}
		supersedes := flat(strings.Join(got.Supersedes, " "))
		for _, want := range []string{"§2.10", "§3.4", "§21.2", "§22.6", "TEMPLATE'S ZEROS"} {
			if !strings.Contains(supersedes, want) {
				t.Errorf("supersedes no longer names %q", want)
			}
		}
	})
}
