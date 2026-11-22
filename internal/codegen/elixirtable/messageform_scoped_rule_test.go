package elixirtable

// SF-23 (docs/silent-failure-audit-1122.md): save_messages held an integer to
// the union of every declaration that shares its field name and kind, so
// A.hp = 6 passed on the strength of B.hp's range. Declarations that share a
// name and a kind share one wire entry, but each holds its own value to its own
// declaration (docs/SPEC.md section 5.1 row 1; docs/SPEC-TABLES.md section
// 3.3). Here A.hp is 0..5 and B.hp is 0..10: 6 is refused under A and written
// under B, and 5 is written under A.
//
// Unit-test rules: no real time, no socket, no network.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const scopedRuleSchema = `package scopedrule

fixed table A
{
    hp int32 | min = 0, max = 5
}

fixed table B
{
    hp int32 | min = 0, max = 10
}

fixed table R
{
    a A
    b B
}
`

func TestElixirSaveMessagesHoldsEachDeclarationToItsOwnRange(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	u := unitFrom(t, scopedRuleSchema)
	vocab := tablewire.MessageVocabulary(u)
	refOf := func(f *ir.Field) int {
		e := ir.TableFieldEntry(f)
		for i, v := range vocab {
			if v.Id == e.Id && v.Kind == e.Kind {
				return i + 1
			}
		}
		t.Fatalf("the vocabulary announces no entry for %s", f.Name)
		return 0
	}
	a := refOf(u.Tables["R"].Fields[0])
	b := refOf(u.Tables["R"].Fields[1])
	hp := refOf(u.Tables["A"].Fields[0])
	if hp != refOf(u.Tables["B"].Fields[0]) {
		t.Fatal("A.hp and B.hp no longer share one wire entry, so this test checks nothing")
	}

	ann := filepath.Join(t.TempDir(), "announce.bin")
	if err := os.WriteFile(ann, depthBoundaryEncodeAnnouncement(ir.BuildVersion(u), ir.TableVocabularyBytes(vocab)), 0o600); err != nil {
		t.Fatal(err)
	}
	row := func(field, value int, want string) string {
		return fmt.Sprintf(`
    r = M.save_messages(v, [[{%[1]d, [{%[2]d, %[3]d}]}]])
    check(%[4]s, "field ref %[1]d holding hp %[3]d: got #{inspect(r)}")
`, field, hp, value, map[string]string{"refused": "match?({:error, _}, r)", "wrote": "match?({:ok, _}, r)"}[want])
	}
	var body strings.Builder
	body.WriteString(`    vocab =
      with {:ok, x} <- M.announce_read(File.read!(file())) do
        x
      else
        other -> raise "announce_read refused the announcement: #{inspect(other)}"
      end

    {:ok, v} = M.for_root(vocab, "R")
`)
	body.WriteString(row(a, 6, "refused"))
	body.WriteString(row(a, 5, "wrote"))
	body.WriteString(row(b, 6, "wrote"))
	body.WriteString(row(b, 7, "wrote"))
	body.WriteString(row(b, 11, "refused"))
	out, err := runMessageProbe(t, u, ann, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
