package javatable

import (
	"strings"
	"testing"
)

// TestViewConstantCarriesItsSignedValueJava holds §8.3's sentence about a
// constant's integer column on the Java leg: int_value is the folded value, so
// the generated registry carries a negative constant with its sign, not its
// magnitude (the Go leg's TestViewConstantListsItsSignedValue, same probe).
func TestViewConstantCarriesItsSignedValueJava(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
const Neg = -5
const Big = 9223372036854775807
fixed table Root
{
    value int32 = 0
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	for _, want := range []string{
		`TableConstantInfo.of("Neg", "probe.schema", "int32", false, -5L,`,
		`TableConstantInfo.of("Big", "probe.schema", "int64", false, 9223372036854775807L,`,
	} {
		name := strings.SplitN(strings.SplitN(want, `"`, 3)[1], `"`, 2)[0]
		line := ""
		for candidate := range strings.SplitSeq(all.String(), "\n") {
			if strings.Contains(candidate, `TableConstantInfo.of("`+name+`"`) {
				line = strings.TrimSpace(candidate)
				break
			}
		}
		if line == "" {
			t.Fatalf("the Java registry carries no row for constant %s", name)
		}
		if !strings.Contains(line, strings.SplitN(want, ", false, ", 2)[1]) {
			t.Errorf("the Java registry lists constant %s as %q, want %q (docs/SPEC-TABLES.md §8.3: int_value is the folded value)", name, line, want)
		}
	}
}
