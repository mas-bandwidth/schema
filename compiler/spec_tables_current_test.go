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
	"strings"
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

	// the page wraps at a fixed width, so a sentence is matched with its line
	// breaks folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	holds := func(t *testing.T, what, text, want string) {
		t.Helper()
		if !strings.Contains(flat(text), flat(want)) {
			t.Errorf("%s: the section does not state %q", what, want)
		}
	}
	omits := func(t *testing.T, what, text, stale string) {
		t.Helper()
		if strings.Contains(flat(text), flat(stale)) {
			t.Errorf("%s: the section still states the retired sentence %q", what, stale)
		}
	}

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
				"STATUS: EIGHT LEGS STILL WRITE FORM `1` AT A DECLARED FIXED ROOT",
				"Elixir emits the fixed form alone",
				"C, C++, C#, Dart, Go, Java, JavaScript and Rust still emit a form-`1` `<T>Save`",
				"Go's plain `<T>Load` and `ReadStrict` already refuse form `1`",
				"C, C++, C#, Dart, Java, JavaScript and Rust still read form `1`",
			},
			stales: []string{
				"EVERY LEG STILL CARRIES THE FORM-`1` PAIR",
				"Beside it, every leg still emits",
				"STATUS: SEVEN LEGS STILL WRITE FORM `1`",
				"STATUS: SIX LEGS STILL WRITE FORM `1`",
				"Dart and Elixir emit the fixed form alone",
				"C, C++, C#, Go, Java and JavaScript still emit",
				"C, C++, C#, Java and JavaScript still read",
			},
		},
		{
			row:  "§3.3 a text list is a variable-table construct",
			text: s33,
			wants: []string{
				"A text list (§2.13) is a VARIABLE-table construct",
				"a fixed table that declares one is refused by the compiler",
				"no message vocabulary carries its shape",
			},
			stales: []string{
				"so a unit that declares one announces one",
				"a Go unit with a text list refused its own announcement",
				"A target that does not yet carry text lists",
			},
		},
		{
			row:  "§3.3 a repeated field id is damage",
			text: s33,
			wants: []string{
				"the same field id twice in one body",
				"last-wins is retired: a repeated field id is damage in every body, this form's included",
			},
			stales: []string{},
		},
		{
			row:  "§4.2 a measure is a framing bound",
			text: s42,
			wants: []string{
				"A measure is a FRAMING bound and not a verdict",
				"it may answer a size for input the `Load` refuses",
			},
			stales: []string{},
		},
		{
			row:  "§6.5 the refusal vocabulary is recommended",
			text: s65,
			wants: []string{
				"the accelerators' recommended vocabulary",
				"the vocabulary is RECOMMENDED and per-language",
				"the verdict, not the name, is the contract",
				"The vocabulary's rule is §7's",
			},
			stales: []string{
				"it is the SAME ENUM the accelerators'",
				"One enum covers both",
				"two enums would be two vocabularies for one answer in nine ports",
				"`TableRefuseReason` is the reference's closed enum",
			},
		},
		{
			row:  "§6.6 the retain walk nests to 128",
			text: s66,
			wants: []string{
				"The walk's nesting cap is 128 nested bodies, §4's one binary bound",
				"a conforming file may nest to 128",
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
				"C++, C, C#, Dart, Elixir, Go, Goose, Java, JavaScript, Lobster, Lua, Luau, MOO, Rust and Squirrel",
				"ELIXIR HAS A WALK TO MEASURE",
				"it emits the text form and both table wires",
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
				"A ranged integer, or a `fixed`/`ufixed` raw value, off its `min`/`max`",
				"A `bits(N)` value wider than `N` bits",
				"A counted array's count past its `Max` or below its `Min`",
				"A `string(N)` or `bytes(N)` length past `N`",
				"A compressed or ranged float off its range",
				"§5.1 row 10",
			},
			stales: []string{},
		},
		{
			row:  "§16.12 no leg caps a number token",
			text: s1612,
			wants: []string{
				"A number token has no length limit in the verdict, and NO LEG MAY CAP IT",
				"no leg may cap a number token",
			},
			stales: []string{
				"a walk's token cap (511 bytes in Go) is a caller resource limit",
				"a token cap is a caller resource limit, outside it",
			},
		},
		{
			row:  "§16.14 W0 every §5.1 row the text writer refuses",
			text: w0,
			wants: []string{
				"which is every row docs/SPEC.md §5.1 lists for the text writer",
				"a `bits(N)` value wider than `N` bits",
				"a compressed or ranged float off its range",
				"a blob named from two slots",
				"a `&node` definition that would hold no field",
			},
			stales: []string{
				"§16.3's list, a deprecated enum variant",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			holds(t, r.row, r.text, want)
		}
		for _, stale := range r.stales {
			omits(t, r.row, r.text, stale)
		}
	}

	runtime, err := os.ReadFile("../tables/goruntime/goruntime.go")
	if err != nil {
		t.Fatal(err)
	}
	comment := string(runtime)
	holds(t, "goruntime refusal comment", comment,
		"recommended table order so a port that follows it spells them alike")
	holds(t, "goruntime refusal comment", comment,
		"verdict, not the names, is the contract")
	omits(t, "goruntime refusal comment", comment,
		"names, order and spelling agree in all ports")

	// THE GENERATED GO TREE CARRIES THE SAME COMMENT, from the same emitter
	// (make/go.mk, make generated-current). Pin the committed copies, so a
	// hand edit to the shared runtime alone cannot leave them stale.
	for _, path := range []string{
		"../generated/bench/tables/go/BenchTableTable.go",
		"../generated/bench/paired/go/BenchTable.go",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tree := string(data)
		holds(t, path, tree,
			"recommended table order so a port that follows it spells them alike")
		holds(t, path, tree,
			"verdict, not the names, is the contract")
		omits(t, path, tree,
			"names, order and spelling agree in all ports")
	}
}
