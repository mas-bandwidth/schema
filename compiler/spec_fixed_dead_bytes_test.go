// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO ON WRITE AND REFUSED IF NON-ZERO
// ON READ (docs/SPEC-TABLES.md §3.4 and §13.10, docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.1, §4.5, §4.6, §5.3 and §5.4, docs/rulings.json
// fixed-dead-bytes-are-zero).
//
// The rule, dated 2026-11-10 ("Every dead byte"): every dead byte in a fixed
// record is zero on write and refused if non-zero on read. The four kinds are an absent
// optional's payload (present byte 0), a counted array's element slots past
// its live count, a string(N)'s or bytes(N)'s bytes past its used length, and
// an inactive union arm (the arm bytes under tag 0, and the bytes past a
// narrower taken arm inside the widest arm's span). The check is the CHECK
// pass of §5.3 step 11, over every record and before any record lands, and the
// verdict is the contract: a non-zero dead byte refuses the whole load. LIVE
// stays the definition of what is read as a value; everything not live in a
// record is dead. An optional nested inside an absent payload, and a nested
// type inside a dead array slot or a dead arm, are dead whole. A fixed array
// [N]T and an enum-keyed array have no dead bytes. A deprecated slot is NOT a
// dead byte.
package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type deadBytesRulingEntry struct {
	ID         string   `json:"id"`
	Date       string   `json:"date"`
	Quotes     []string `json:"quotes"`
	Supersedes []string `json:"supersedes"`
}

func TestSpecFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	const tables = "../docs/SPEC-TABLES.md"
	const algorithm = "../docs/FIXED-FORM-ALGORITHM.md"
	// the maintainer's words, verbatim: first on an absent optional's bytes,
	// then on whether the rule held for every dead byte
	wantQuotes := []string{"take the strict approach and refuse.", "Every dead byte"}

	record := specSection(t, tables, "#### THE RECORD", "#### SELECTION IS BY THE KEYWORD")
	oneRead := specSection(t, tables, "### 13.10 The one read, ruled", "## 14. Design notes")

	write := specSection(t, algorithm, "### 3.1 The write", "## 4. The reader: ONE path")
	plan := specSection(t, algorithm, "### 4.1 The plan", "### 4.2 Which plan")
	scatter := specSection(t, algorithm, "### 4.5 The scatter", "### 4.6 The bounds pass")
	bounds := specSection(t, algorithm, "### 4.6 The bounds pass", "## 5. Evolution")
	load := specSection(t, algorithm, "### 5.3 LOAD", "### 5.4 The counter")
	perOp := specSection(t, algorithm, "### 5.4 The counter", "### 5.5 The closure")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§3.4 THE RECORD: the four kinds are dead and refused",
			text: record,
			wants: []string{
				"DEAD",
				"wire_damaged",
				"§5.3 step 11",
				"an absent optional's payload (present byte `0`)",
				"a counted array's element slots past its live count",
				"a `string(N)`'s or `bytes(N)`'s bytes past its used length",
				"an inactive union arm",
				"fixed array `[N]T`",
				"enum-keyed array",
				"DEPRECATED SLOT is not a dead byte",
			},
			stales: []string{
				"UNSPECIFIED on read",
				"IGNORED on read",
				"never a refusal",
			},
		},
		{
			row:  "§13.10: the dead-byte exception is gone",
			text: oneRead,
			wants: []string{
				"DEAD",
				"zero on write",
				"REFUSES the whole load as `wire_damaged`",
				"§5.3 step 11",
			},
			stales: []string{
				"What it keeps on purpose, as an exception",
				"Non-zero slack in a fixed record is unread and not refused",
			},
		},
		{
			row:  "§3.1: the read refuses a non-zero dead byte",
			text: write,
			wants: []string{
				"Dead bytes are ZERO on read",
				"wire_damaged",
				"inactive union arm",
				"§5.3 step 11",
			},
			stales: []string{
				"Slack is UNSPECIFIED on read",
				"non-zero slack is not damage",
				"not a refusal",
			},
		},
		{
			row:  "§4.1: a present op, and what decides the dead bytes",
			text: plan,
			wants: []string{
				"`present`",
				"constant `1`",
				"A PRESENT BYTE, A COUNT, A LENGTH AND A TAG DECIDE WHICH BYTES ARE DEAD",
				"fixed array `[N]T`",
				"enum-keyed",
				"DEPRECATED SLOT is not a dead byte",
				"wire_damaged",
			},
		},
		{
			row:  "§4.5: a count and a length leave dead bytes",
			text: scatter,
			wants: []string{
				"DEAD",
				"wire_damaged",
				"past its count",
				"past the used length",
				"inactive union arm",
				"present",
			},
		},
		{
			row:  "§4.6: the bounds pass walks live data, the check pass holds dead bytes",
			text: bounds,
			wants: []string{
				"DEAD",
				"CHECK pass",
				"§5.3 step 11",
			},
			stales: []string{
				"never slack",
			},
		},
		{
			row:  "§5.3 step 11 and LIVE: everything not live is dead",
			text: load,
			wants: []string{
				"every DEAD byte",
				"REFUSE wire_damaged",
				"EVERYTHING NOT LIVE IS DEAD",
				"fixed array `[N]T`",
				"DEPRECATED SLOT is not a dead byte",
				"before any record lands",
			},
			stales: []string{
				"over LIVE data only",
			},
		},
		{
			row:  "§5.4: the CHECK loop covers the dead bytes",
			text: perOp,
			wants: []string{
				"non-zero DEAD byte",
				"`present`",
			},
			stales: []string{
				"over LIVE data only",
			},
		},
	}

	// the pages wrap at a fixed width, so a sentence is matched with its line
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

	t.Run("the ruling is recorded with the sentences it supersedes", func(t *testing.T) {
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
			if file.Rulings[i].ID == "fixed-dead-bytes-are-zero" {
				got = &file.Rulings[i]
				break
			}
		}
		if got == nil {
			t.Fatal("docs/rulings.json carries no ruling fixed-dead-bytes-are-zero")
		}
		if got.Date != "2026-11-10" {
			t.Errorf("date = %q, want 2026-11-10", got.Date)
		}
		for _, want := range wantQuotes {
			found := false
			for _, quote := range got.Quotes {
				if quote == want {
					found = true
				}
			}
			if !found {
				t.Errorf("the ruling does not quote %q verbatim; quotes = %q", want, got.Quotes)
			}
		}
		supersedes := flat(strings.Join(got.Supersedes, " "))
		for _, want := range []string{
			"SPEC-TABLES.md §3.4",
			"§13.10",
			"FIXED-FORM-ALGORITHM.md §3.1",
			"§4.1",
			"§4.5",
			"§4.6",
			"§5.3 step 11",
			"§5.4",
		} {
			if !strings.Contains(supersedes, want) {
				t.Errorf("supersedes no longer names %q; got %q", want, supersedes)
			}
		}
	})
}
