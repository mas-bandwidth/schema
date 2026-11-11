// TestSpecTablesCurrent pins docs/SPEC-TABLES.md and the shared Go runtime's
// refusal comment to the rulings the page states: the refusal verdict is the
// contract and its vocabulary is recommended and per-language
// (refusal-verdict-only), input no conforming writer can produce is refused
// (strict-only-read), only fixed tables carry the message form
// (message-form-fixed-only), every read at a fixed root refuses form 1
// (fixed-root-refuses-form1), and a measure is a framing bound and not a
// verdict (conformance-is-read-then-write). Every want below is a corrected
// statement and every stale a removed phrase.
package compiler

import (
	"os"
	"testing"
)

func TestSpecTablesCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	status := specTablesSection(t, page, "**STATUS:", "**How to choose.**")
	s33 := specTablesSection(t, page, "### 3.3 The message form", "### 3.4 The fixed form")
	s42 := specTablesSection(t, page, "### 4.2 The read is the verifier", "### 4.3 One-sided integer ranges")
	s65 := specTablesSection(t, page, "### 6.5 The two load paths", "### 6.6 Retain-unknown")
	s66 := specTablesSection(t, page, "### 6.6 Retain-unknown", "## 7. The cooked form")
	s161 := specTablesSection(t, page, "### 16.1 The surface", "### 16.2 The mapping")
	s163 := specTablesSection(t, page, "### 16.3 What the writer refuses", "### 16.4 The key")
	s1612 := specTablesSection(t, page, "### 16.12 The outside oracle", "### 16.13 Any JSON value")
	w0 := specTablesSection(t, page, "**W0. The result is the whole text or nothing.**", "**W1. The graph pass**")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "the three wires' status",
			text: status,
			wants: []string{
				"STATUS: SIX LEGS STILL WRITE FORM `1` AT A DECLARED FIXED ROOT",
				"Dart, Elixir and Rust emit the fixed form alone",
				"C, C++, C#, Go, Java and JavaScript still emit a form-`1` `<T>Save`",
				"Go's plain `<T>Load` and `ReadStrict` already refuse",
				"C, C++, C#, Java and JavaScript still read form `1`",
			},
			stales: []string{
				"EVERY LEG STILL CARRIES THE FORM-`1` PAIR",
				"Beside it, every leg still emits",
			},
		},
		{
			row:  "§3.3 a text list is a variable-table construct",
			text: s33,
			wants: []string{
				"A text list (§2.13) is a VARIABLE-table construct",
				"no message\nvocabulary carries it",
				"a fixed table that declares one is refused by the\ncompiler",
			},
			stales: []string{
				"so a unit\nthat declares one announces one",
				"(a Go unit with a text\nlist refused its own announcement until this rule was written down)",
				"A target that does not\nyet carry text lists",
			},
		},
		{
			row:  "§3.3 a repeated field id is damage",
			text: s33,
			wants: []string{
				"A FIELD ID REPEATED IN ANY BODY, this form's included",
				"no read lets a",
				"later occurrence win",
			},
			stales: []string{
				"The five ways a batch is damaged",
			},
		},
		{
			row:  "§4.2 a measure is a framing bound",
			text: s42,
			wants: []string{
				"A measure is a FRAMING bound and not a",
				"verdict: it may answer a size for input the `Load` refuses",
			},
			stales: []string{},
		},
		{
			row:  "§6.5 the refusal vocabulary is recommended",
			text: s65,
			wants: []string{
				"the SAME RECOMMENDED VOCABULARY",
				"the VERDICT is the contract",
				"the vocabulary is\n  RECOMMENDED and per-language",
			},
			stales: []string{
				"it is the SAME ENUM the accelerators'",
				"two enums would be two vocabularies\n  for one answer in nine ports",
				"`TableRefuseReason` is the reference's closed enum",
				"a port is RECOMMENDED\n  to spell the same members",
			},
		},
		{
			row:  "§6.6 the retain walk nests to 128",
			text: s66,
			wants: []string{
				"The walk's nesting cap is 128 nested bodies, §4's one binary bound",
			},
			stales: []string{
				"SMALL STATED CONSTANT: 64 nested bodies",
			},
		},
		{
			row:  "§16.1 which legs emit the text form",
			text: s161,
			wants: []string{
				"the FIXED class in every table backend",
				"C, C++, C#, Dart, Elixir, Go, Goose, Java, JavaScript, Lobster, Lua, Luau, MOO,",
				"Rust and Squirrel — and the VARIABLE class",
				"ELIXIR HAS A WALK TO MEASURE",
				"it emits the text form and the table wire",
			},
			stales: []string{
				"the FIXED class in all nine",
				"ELIXIR HAS NO WALK TO MEASURE",
				"it emits no text form and no table wire",
			},
		},
		{
			row:  "§16.3 every §5.1 row the text writer refuses",
			text: s163,
			wants: []string{
				"a ranged integer or a `fixed`/`ufixed` raw value off its\n  `min`/`max`",
				"a `bits(N)` value wider than `N` bits",
				"a counted array's count\n  past its `Max` or below its `Min`",
				"a `string(N)` or `bytes(N)` length past\n  `N`",
				"a compressed or ranged float off its range",
				"the same rows\n  docs/SPEC.md §5.1 lists for the text writer",
			},
			stales: []string{},
		},
		{
			row:  "§16.12 no leg caps a number token",
			text: s1612,
			wants: []string{
				"a token's length is never the reason it is refused",
				"NO LEG MAY CAP IT",
				"no number-length limit is part of the verdict and no leg may cap a number token",
			},
			stales: []string{
				"a walk's token cap (511 bytes in Go) is a caller",
				"a caller\nresource limit, outside the verdict contract",
				"a token cap is a caller resource limit",
			},
		},
		{
			row:  "§16.14 W0 every §5.1 row the text writer refuses",
			text: w0,
			wants: []string{
				"which is every row docs/SPEC.md\n§5.1 lists for the text writer",
				"a `bits(N)` value wider than `N` bits",
				"a compressed or ranged float off its\nrange",
				"a blob named from two slots",
				"a `&node`\ndefinition that would hold no field",
			},
			stales: []string{
				"§16.3's list, a deprecated enum variant",
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

	runtime, err := os.ReadFile("../tables/goruntime/goruntime.go")
	if err != nil {
		t.Fatal(err)
	}
	comment := string(runtime)
	mustState(t, "goruntime refusal comment", comment,
		"recommended table order so a port that follows it spells them alike")
	mustState(t, "goruntime refusal comment", comment,
		"the verdict,\n\t// not the names, is the contract")
	mustNotState(t, "goruntime refusal comment", comment,
		"names, order and spelling agree in all ports")
}
