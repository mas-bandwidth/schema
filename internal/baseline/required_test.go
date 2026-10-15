// The `| required` token in the tables baseline (docs/SPEC-TABLES.md §2.3):
// adding it to a shipped field, or a new field carrying it, is refused;
// removing it passes; the token round-trips through the file.
package baseline_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const requiredRowsSrc = `package rows

table Ship
{
    name  string(32)
    score int32 | required
}
`

func TestRequiredIsJudgedByTheTablesBaseline(t *testing.T) {
	t.Parallel()
	plain := editOf(t, requiredRowsSrc, "score int32 | required", "score int32")
	t.Run("added to a shipped field is refused", func(t *testing.T) {
		got := baseline.Diff(committed(t, plain), baseline.Render(unit(t, requiredRowsSrc)), baseline.DefaultTokenPolicy)
		if !find(got, baseline.Refuse, "Ship.score", "required added") {
			t.Fatalf("adding required must be refused, got:%s", summary(got))
		}
		if ctrl := baseline.Diff(committed(t, plain), baseline.Render(unit(t, requiredRowsSrc)), without("required")); len(ctrl) != 0 {
			t.Errorf("with the required row removed the edit passes, got:%s", summary(ctrl))
		}
	})
	t.Run("removed is free", func(t *testing.T) {
		got := baseline.Diff(committed(t, requiredRowsSrc), baseline.Render(unit(t, plain)), baseline.DefaultTokenPolicy)
		if len(got) != 0 {
			t.Fatalf("removing required is free, got:%s", summary(got))
		}
	})
	t.Run("a new required field is refused, a new plain one is free", func(t *testing.T) {
		base := editOf(t, requiredRowsSrc, "    score int32 | required\n", "")
		withReq := baseline.Diff(committed(t, base), baseline.Render(unit(t, requiredRowsSrc)), baseline.DefaultTokenPolicy)
		if !find(withReq, baseline.Refuse, "Ship.score", "new field is `| required`") {
			t.Fatalf("a new required field must be refused, got:%s", summary(withReq))
		}
		withPlain := baseline.Diff(committed(t, base), baseline.Render(unit(t, plain)), baseline.DefaultTokenPolicy)
		if len(withPlain) != 0 {
			t.Fatalf("a new plain field is free, got:%s", summary(withPlain))
		}
	})
	t.Run("round trips through the file", func(t *testing.T) {
		live := baseline.Render(unit(t, requiredRowsSrc))
		back, err := baseline.Parse("tables.baseline", []byte(live.Text()))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := baseline.Diff(back, live, baseline.DefaultTokenPolicy); len(got) != 0 {
			t.Fatalf("parsed file diffs against its rendering:%s", summary(got))
		}
	})
}
