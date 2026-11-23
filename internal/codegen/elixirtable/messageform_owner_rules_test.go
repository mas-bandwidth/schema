package elixirtable

// THE MESSAGE WRITER HOLDS A VALUE TO ITS OWN DECLARATION (docs/SPEC.md §5.1
// row 1; docs/SPEC-TABLES.md §3.3; SF-23). Two declarations that share a field
// name and kind share ONE wire entry, announced at the looser span, but each
// declaration still holds its own value to its own range: A.hp = 6 is refused
// on A.hp being 0..5, never admitted on B.hp being 0..9. Every refused value
// has a lawful twin: the same value under the declaration that admits it.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const ownerRulesSchema = `package ownrule

fixed table A { hp uint8 | min = 0, max = 5 }

fixed table B { hp uint8 | min = 0, max = 9 }

fixed table Root
{
    a A
    b B
}
`

func ownerRulesProbe(t *testing.T) (*ir.Unit, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "OwnRule.schema")
	if err := os.WriteFile(path, []byte(ownerRulesSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := messageLoadUnit(t, dir, []string{"OwnRule.schema"})
	vocab := tablewire.MessageVocabulary(u)
	refOf := func(table, field string) int {
		var want ir.TableVocabularyEntry
		for _, f := range u.Tables[table].Fields {
			if f.Name == field {
				want = ir.TableFieldEntry(f)
			}
		}
		for i, e := range vocab {
			if e.Key() == want.Key() {
				return i + 1
			}
		}
		t.Fatalf("the vocabulary has no %s.%s", table, field)
		return 0
	}
	a, b, hpA, hpB := refOf("Root", "a"), refOf("Root", "b"), refOf("A", "hp"), refOf("B", "hp")
	idA, idB := ir.TableFieldEntry(u.Tables["A"].Fields[0]), ir.TableFieldEntry(u.Tables["B"].Fields[0])
	if idA.Id != idB.Id || idA.Kind != idB.Kind {
		t.Fatalf("A.hp and B.hp do not share a wire id and kind: the probe is vacuous")
	}
	var body strings.Builder
	body.WriteString("    {:ok, announced} = M.announce_read(M.announce())\n    {:ok, vocab} = M.for_root(announced, \"Root\")\n")
	for _, c := range []struct {
		owner int
		hp    int
		value int
		ok    bool
		what  string
	}{
		{a, hpA, 5, true, "A.hp at its own max"},
		{a, hpA, 6, false, "A.hp past its own max, inside B.hp"},
		{b, hpB, 6, true, "B.hp at 6, inside its own range"},
		{b, hpB, 9, true, "B.hp at its own max"},
		{b, hpB, 10, false, "B.hp past its own max"},
	} {
		verdict := "{:error, :invalid_value}"
		if c.ok {
			verdict = "{:ok, _}"
		}
		fmt.Fprintf(&body, "    check(match?(%s, M.save_messages(vocab, [[{%d, [{%d, %d}]}]])), %q)\n", verdict, c.owner, c.hp, c.value, c.what)
		fmt.Fprintf(&body, "    check(match?(%s, M.measure_messages(vocab, [[{%d, [{%d, %d}]}]])), %q)\n", verdict, c.owner, c.hp, c.value, "measure: "+c.what)
	}
	return u, body.String()
}

func TestElixirSaveMessagesHoldsEachDeclarationToItsOwnRange(t *testing.T) {
	t.Parallel()
	u, body := ownerRulesProbe(t)
	out, err := runMessageProbe(t, u, filepath.Join(t.TempDir(), "announce.bin"), body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
