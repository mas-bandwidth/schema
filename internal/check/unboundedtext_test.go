// Unbounded text in the checker (docs/SPEC-TABLES.md §2.13.1): the five
// spellings accepted in a variable table through the one validating path, and
// each refusal the section states, matched by its diagnostic text.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestUnboundedTextSpellingsAccepted(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, "package t\ntable Where {\n    rules []string\n    first []?string\n    recent [..4]string\n    pending [..4]?string\n    byname map[string]int32\n}\n")
	got := ir.TableUnboundedTexts(u)
	want := []string{"Where.byname", "Where.first", "Where.pending", "Where.recent", "Where.rules"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unbounded texts %v, want %v", got, want)
	}
	w := u.Tables["Where"]
	for _, f := range w.Fields[:4] {
		if !ir.TextListUnbounded(f) {
			t.Fatalf("%s is not an unbounded text list", f.Name)
		}
		if v := ir.TextListValue(f); v.Type.Size != 0 || !v.Type.TextUnbounded() {
			t.Fatalf("%s: the element is not unbounded text: %+v", f.Name, v.Type)
		}
	}
	if !ir.TextListOptional(w.Fields[1]) || !ir.TextListPlain(w.Fields[0]) || w.Fields[2].ArrayBound != 4 || w.Fields[0].Array != ir.ArrayList {
		t.Fatal("the element records do not describe the spellings")
	}
	if !ir.MapKeyUnbounded(w.Fields[4]) || !ir.UnitMapKeyUnbounded(u, "Where.byname") {
		t.Fatal("the map key is not unbounded text")
	}
	// the wire is the bounded spelling's: element kind 12 and 13, key kind 12
	if ir.TableElemKind(w.Fields[0]) != ir.TableKindString || ir.TableElemKind(w.Fields[1]) != ir.TableKindTable {
		t.Fatal("element kinds: a plain list is 12 and a ? list is 13 (§2.13.1)")
	}
	if ir.TableScalarKind(ir.MapKeyField(w.Fields[4])) != ir.TableKindString {
		t.Fatal("an unbounded key is kind 12")
	}
	// a bounded unit holds none
	if b := buildUnit(t, "package t\ntable L {\n    rules []string(8)\n    m map[string(8)]int32\n}\n"); len(ir.TableUnboundedTexts(b)) != 0 || ir.UnitHasUnboundedText(b) {
		t.Fatal("a bounded unit reports unbounded text")
	}
}

func TestUnboundedTextRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, want, src string }{
		{"a plain field", "field name: a bare `string` has no bound, and only a text-list element ([]string, [..N]string) and a map key (map[string]V) may be unbounded text; declare string(N) for inline text, or *string",
			"package t\ntable T {\n    name string\n}\n"},
		{"[N]string", "field rules: [N]string is refused — a text list is COUNTED, so a fixed extent, an enum key or a lower bound names a count that cannot vary the way a list's does; spell it [..N]string",
			"package t\ntable T {\n    rules [4]string\n}\n"},
		{"[E]string", "field rules: [E]string is refused — a text list is COUNTED",
			"package t\nenum E { A, B }\ntable T {\n    rules [E]string\n}\n"},
		{"?[]string", "field rules: ?[]string puts a presence bit on a list of text",
			"package t\ntable T {\n    rules ?[]string\n}\n"},
		{"?[..N]string", "field rules: ?[..N]string puts a presence bit on a list of text",
			"package t\ntable T {\n    rules ?[..4]string\n}\n"},
		{"a type body", "field name: a bare `string` in a `type` body or a packet is refused — the type wire carries no unbounded text",
			"package t\ntype P {\n    name string\n}\n"},
		{"a text list in a type body", "field rules: []string is a text list, and a text list is a VARIABLE-table construct",
			"package t\ntype P {\n    rules []string\n}\n"},
		{"a packet (a type nested in a type)", "field name: a bare `string` in a `type` body or a packet is refused",
			"package t\ntype Inner {\n    name string\n}\ntype P {\n    inner Inner\n}\n"},
		{"a union arm", "union U: arm word: a bare `string` is not an arm",
			"package t\nunion U {\n    word string\n}\ntable H {\n    u U\n}\n"},
		{"a map value", "field value: a map VALUE of bare `string` is refused",
			"package t\ntable T {\n    m map[uint8]string\n}\n"},
		{"a text list as a map value", "a text list is not a map value",
			"package t\ntable T {\n    m map[uint8][]string\n}\n"},
		{"map[?string]T", "an OPTIONAL key is refused",
			"package t\ntable T {\n    m map[?string]int32\n}\n"},
		{"a fixed table field", "fixed table F: field name is a bare `string`, and unbounded text is a VARIABLE-table construct — declare string(N), or drop `fixed` from F and hold the text as []string or a map[string]V key in a variable table",
			"package t\nfixed table F {\n    name string\n}\n"},
		{"a fixed table unbounded list", "fixed table F: field rules is a text list ([..N]string), and a text list is a VARIABLE-table construct",
			"package t\nfixed table F {\n    rules [..4]string\n}\n"},
		{"bytes stays bounded", "expected (",
			"package t\ntable T {\n    xs []bytes\n}\n"},
		{"*string list stays a follow-on", "an array of byte buffers is a named follow-on",
			"package t\ntable T {\n    xs []*string\n}\n"},
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
