package cpptable

import (
	"strings"
	"testing"
)

const optionalVariableCookSchema = `package probe

table Inner
{
    text *string
    items []int32
}

table Outer
{
    pair ?[2]Inner
    many ?[..2]Inner
}
`

// TestCookOptionalVariableClosureSource holds the cook half of §2.3: after
// writing an optional's presence companion, neither the record writer nor the
// extent writer may visit stale storage while that companion is false.
func TestCookOptionalVariableClosureSource(t *testing.T) {
	t.Parallel()
	source := t03Header(t, t03Unit(t, optionalVariableCookSchema))
	for _, guard := range []string{
		"if ( value.pair_present )",
		"if ( value.many_present )",
	} {
		if got := strings.Count(source, guard); got < 2 {
			t.Fatalf("cook source has %d %q guards, want record and extent guards", got, guard)
		}
	}
}
