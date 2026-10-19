package cstable

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

// TestCookOptionalVariableClosureSource holds the cook half of §2.3 for both
// managed values and region values: absent storage is skipped by the extent
// sizing pass and by the pack pass after its false presence byte is written.
func TestCookOptionalVariableClosureSource(t *testing.T) {
	t.Parallel()
	files := generateFiles(t, "Probe", optionalVariableCookSchema)
	var source strings.Builder
	for _, data := range files {
		source.Write(data)
	}
	for _, guard := range []string{
		"if (f.Optional && !f.GetPresent(value)) { return true; }",
		"if(f.Optional&&!value.Present(f)) { return true; }",
	} {
		if got := strings.Count(source.String(), guard); got < 2 {
			t.Fatalf("cook source has %d %q guards, want extent and pack guards", got, guard)
		}
	}
}
