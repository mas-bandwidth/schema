package javatable

import (
	"strings"
	"testing"
)

func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()

	refused := []struct {
		name        string
		definition  string
		wantRefuses []string
	}{
		{
			name: "map",
			definition: `package probe
table Child {}
table BadMap { m map[int32]Child }
`,
			wantRefuses: []string{"BadMap", "m", "map"},
		},
		{
			name: "pointer to table outside list",
			definition: `package probe
table Child {}
table BadPointer { c *Child }
`,
			wantRefuses: []string{"BadPointer", "c", "pointer"},
		},
		{
			name: "optional ?T",
			definition: `package probe
table Child {}
table BadOptional { c ?Child }
`,
			wantRefuses: []string{"BadOptional", "c", "optional"},
		},
		{
			name: "enum-keyed array",
			definition: `package probe
enum Col { red, blue }
table BadKeyed { k [Col]int32 }
`,
			wantRefuses: []string{"BadKeyed", "k", "enum-keyed"},
		},
		{
			name: "*bytes",
			definition: `package probe
table BadBytes { b *bytes }
`,
			wantRefuses: []string{"BadBytes", "b", "*bytes"},
		},
		{
			name: "union in a table closure",
			definition: `package probe
table Child {}
union U
{
    n int32
    c Child
}
table BadUnion { u U }
`,
			wantRefuses: []string{"BadUnion", "u", "union"},
		},
		{
			name: "wide kind",
			definition: `package probe
table BadWide { w uint128 }
`,
			wantRefuses: []string{"int128"},
		},
	}

	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := refuseVariableForm(unitFrom(t, tc.definition))
			if err == nil {
				t.Fatal("construct should be refused but was not")
			}
			msg := err.Error()
			for _, want := range tc.wantRefuses {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal message %q should contain %q", msg, want)
				}
			}
		})
	}

	// The refusal walks INTO the table elements an unbounded list reaches
	// (docs/SPEC-TABLES.md §2.9): a forbidden construct nested inside a list
	// element's table is refused naming the inner table, the field and the
	// construct, never silently carried by an otherwise-acceptable owner.
	{
		nested := `package probe
table Child { m map[int32]int32 }
table Card { items []Child }
`
		err := refuseVariableForm(unitFrom(t, nested))
		if err == nil {
			t.Fatal("a map nested in a list element should be refused but was not")
		}
		msg := err.Error()
		for _, want := range []string{"Child", "m", "map"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal message %q should contain %q", msg, want)
			}
		}
	}

	// The card's own constructs are NOT refused: a plain table root with bool,
	// int32, int64 and enum fields, a string(N), the *string byte buffer and an
	// unbounded list of table elements (docs/SPEC-TABLES.md §2.5, §2.9).
	card := `package probe
enum Tier { flash, pro, heavy }
table Elem { s string(8) }
table Card
{
    id       string(64)
    tier     Tier
    count    int32
    total    int64
    ok       bool
    done_when *string
    source    *string
    items    []Elem
}
`
	// The boundary rule is asked directly here: the card's own constructs lie
	// inside the variable form, so the refusal names nothing. (The emitters for
	// the variable form itself are the follow-on work; this test pins the
	// boundary, not the codec.)
	if err := refuseVariableForm(unitFrom(t, card)); err != nil {
		t.Fatalf("the card's own constructs are refused: %v", err)
	}
}
