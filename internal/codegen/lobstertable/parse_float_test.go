package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// A NUMBER TOKEN PARSES THE SAME ON EVERY HOST (docs/SPEC-TABLES.md §3.3).
// Lobster's string_to_float is strtod on macOS and std::from_chars on Linux
// and Windows, and from_chars answers 0.0 for a token out of range, where
// strtod and Go's ParseFloat answer an infinity on overflow. The runtime's
// float_out_of_range resolves that zero; this drives it directly with the
// zero from_chars gives, so the rule is checked on a host whose
// string_to_float never reaches it.
func TestLobsterFloatOutOfRangeIsTheRangeEnd(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, finiteVariableSchema)
	cases := []struct{ token, want string }{
		{"1e400", "+inf"},
		{"-1e400", "-inf"},
		{"1.5e308", "+inf"},
		{"18e307", "+inf"},
		{"0.0001e309", "+inf"},
		{"1e-400", "+0"},
		{"-1e-400", "-0"},
		{"0.001e-322", "+0"},
		{"123.45e-500", "+0"},
		{"0", "+0"},
		{"-0.0", "-0"},
		{"0e400", "+0"},
		{"-0.000e-9", "-0"},
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n\n")
	b.WriteString(`def show(x:float) -> string:
    if x != x:
        return "nan"
    let neg = (schema_rt.f64bits(x) & 0x8000000000000000) != 0
    let sign = if neg: "-" else: "+"
    if x == 0.0:
        return sign + "0"
    if x - x != x - x:
        return sign + "inf"
    return "finite"

`)
	for _, c := range cases {
		zero := "0.0"
		if strings.HasPrefix(c.token, "-") {
			zero = "schema_rt.f64frombits(0x8000000000000000)"
		}
		fmt.Fprintf(&b, "print(show(schema_rt.float_out_of_range(%q, %s)))\n", c.token, zero)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
	if len(got) != len(cases) {
		t.Fatalf("want %d answers, got %d:\n%s", len(cases), len(got), strings.Join(got, "\n"))
	}
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("float_out_of_range(%q) = %s, want %s", c.token, got[i], c.want)
		}
	}
}
