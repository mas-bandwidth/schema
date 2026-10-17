// Text lists and the array root in the checker (docs/SPEC-TABLES.md §2.13,
// §16.9): each refusal the two sections state, matched by its diagnostic text,
// and the four spellings accepted in a variable table.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestTextListSpellingsAccepted(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, "package t\ntable Lists {\n    rules [..4]string(8)\n    first [..4]?string(8)\n    repos []string(8)\n    bases []?string(8)\n}\ntable GoalList | json = \"array\" {\n    goals []string(8)\n}\n")
	got := ir.TableTextLists(u)
	want := []string{"GoalList.goals", "Lists.bases", "Lists.first", "Lists.repos", "Lists.rules"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("text lists %v, want %v", got, want)
	}
	lists := u.Tables["Lists"]
	if !ir.TextListPlain(lists.Fields[0]) || !ir.TextListOptional(lists.Fields[1]) || lists.Fields[0].ArrayBound != 4 || lists.Fields[2].Array != ir.ArrayList {
		t.Fatal("the element records do not describe the spellings")
	}
	if ir.TableElemKind(lists.Fields[0]) != ir.TableKindString || ir.TableElemKind(lists.Fields[1]) != ir.TableKindTable {
		t.Fatal("element kinds: a plain list is 12 and a ? list is 13 (§2.13)")
	}
	if e := u.Tables["ListsRulesText"]; e == nil || !e.IsTextListElem() || e.MapEntryOf != "Lists.rules" {
		t.Fatal("the element record is not generated under its claimed name")
	}
	if !u.Tables["GoalList"].JsonArray {
		t.Fatal("json = \"array\" not recorded")
	}
}

func TestTextListAndArrayRootRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, want, src string }{
		{"text list in a fixed table", "a text list is a VARIABLE-table construct",
			"package t\nfixed table F {\n    rules [..4]string(8)\n}\n"},
		{"text list in a type body", "a text list is a VARIABLE-table construct",
			"package t\ntype P {\n    rules [..4]string(8)\n}\n"},
		{"text list as a map value", "a text list is not a map value",
			"package t\ntable M {\n    m map[string(4)][..4]string(8)\n}\n"},
		{"text list as a union arm", "a text list is not an arm",
			"package t\nunion U {\n    words [..4]string(8)\n}\ntable H {\n    u U\n}\n"},
		{"[N]string(M)", "a text list is COUNTED",
			"package t\ntable L {\n    rules [4]string(8)\n}\n"},
		{"[E]string(M)", "a text list is COUNTED",
			"package t\nenum E { A, B }\ntable L {\n    rules [E]string(8)\n}\n"},
		{"?[..N]string(M)", "puts a presence bit on a list of text",
			"package t\ntable L {\n    rules ?[..4]string(8)\n}\n"},
		{"?[]string(M)", "puts a presence bit on a list of text",
			"package t\ntable L {\n    rules ?[]string(8)\n}\n"},
		{"[..N]?T for a T but string(M)", "a named follow-on for every T but string(M)",
			"package t\ntable L {\n    xs [..4]?int32\n}\n"},
		{"[]?T for a T but string(M)", "a named follow-on for every T but string(M)",
			"package t\ntable L {\n    xs []?int32\n}\n"},
		{"array of wstring(N)", "an array of wstring(N) is not supported",
			"package t\ntable L {\n    xs [..4]wstring(8)\n}\n"},
		{"array of bytes(N)", "an array of bytes(N) is not supported",
			"package t\ntable L {\n    xs [..4]bytes(8)\n}\n"},
		{"array of bits(N)", "an array of bits(N) is not supported",
			"package t\ntable L {\n    xs [..4]bits(3)\n}\n"},
		{"array of *string", "field xs: an array of byte buffers is a named follow-on",
			"package t\ntable L {\n    xs [..4]*string\n}\n"},
		{"array of *bytes", "field xs: an array of byte buffers is a named follow-on",
			"package t\ntable L {\n    xs [..4]*bytes\n}\n"},
		{"a default on a text list", "a text list takes no specified default",
			"package t\ntable L {\n    xs [..4]string(8) = \"a\"\n}\n"},
		{"a bound attribute on a text list", "does not apply to a text list",
			"package t\ntable L {\n    xs [..4]string(8) | max = 3\n}\n"},
		{"the element record name is claimed", "the name is CLAIMED on L by the list",
			"package t\ntable L {\n    xs [..4]string(8)\n}\ntable LXsText {\n    y int32\n}\n"},
		{"the Append accessor is claimed", "generated text-list surface",
			"package t\ntable L {\n    xs [..4]string(8)\n}\ntable LXsAppend {\n    y int32\n}\n"},
		{"json on a table takes array only", "names no text shape",
			"package t\ntable G | json = \"object\" {\n    goals []int32\n}\n"},
		{"json on a table takes a string", "json on a table line takes a quoted string",
			"package t\ntable G | json = 3 {\n    goals []int32\n}\n"},
		{"bare json on a table", "attribute json requires a value",
			"package t\ntable G | json {\n    goals []int32\n}\n"},
		{"array root on a fixed table", "is a VARIABLE-table declaration",
			"package t\nfixed table G | json = \"array\" {\n    goals [..4]int32\n}\n"},
		{"array root with no field", "needs exactly ONE field",
			"package t\ntable G | json = \"array\" {\n}\n"},
		{"array root with two fields", "needs exactly ONE field",
			"package t\ntable G | json = \"array\" {\n    a []int32\n    b []int32\n}\n"},
		{"array root over a non-array", "it is not an array",
			"package t\ntable G | json = \"array\" {\n    a int32\n}\n"},
		{"array root over ?[..N]T", "refuses ?[..N]T",
			"package t\ntable G | json = \"array\" {\n    a ?[..4]int32\n}\n"},
		{"array root over [N]T", "refuses [N]T and [E]T",
			"package t\ntable G | json = \"array\" {\n    a [4]int32\n}\n"},
		{"array root over [E]T", "refuses [N]T and [E]T",
			"package t\nenum E { A, B }\ntable G | json = \"array\" {\n    a [E]int32\n}\n"},
		{"array root with a guarded field", "refuses a guarded (if) field",
			"package t\ntable G | json = \"array\" {\n    on bool\n    if on {\n        a []int32\n    }\n}\n"},
		{"array root field with a json key", "refuses a json key on its one field",
			"package t\ntable G | json = \"array\" {\n    a []int32 | json = \"k\"\n}\n"},
		{"array root as a pointer target", "refuses a table any pointer targets, and H.p points at it",
			"package t\ntable G | json = \"array\" {\n    a []int32\n}\ntable H {\n    p *G\n}\n"},
		{"array root as a list-of-pointers target", "refuses a table any pointer targets, and H.ps points at it",
			"package t\ntable G | json = \"array\" {\n    a []int32\n}\ntable H {\n    ps []*G\n}\n"},
		{"array root as a map value's pointer target", "refuses a table any pointer targets, and H.m.value points at it",
			"package t\ntable G | json = \"array\" {\n    a []int32\n}\ntable H {\n    m map[uint8]*G\n}\n"},
		{"array root as a pointer arm's target", "refuses a table any pointer targets, and H.u.g points at it",
			"package t\ntable G | json = \"array\" {\n    a []int32\n}\nunion U {\n    g *G\n}\ntable H {\n    u U\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}
