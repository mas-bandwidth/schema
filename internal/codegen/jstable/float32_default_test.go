package jstable

import (
	"regexp"
	"strings"
	"testing"
)

// A float32 field's default is float32 of its declaration (SPEC §4.3, "A
// FLOAT LITERAL IS A float64 FIRST"), and JavaScript reads a literal as a
// double: the constructor and the descriptor's def, which an absent field
// reads back, spell float32(0.7) as its float64 value, 0.699999988079071,
// never the decimal 0.7, which is the double 0.7. A float64 field keeps its
// decimal. No shared table surface can see this one: every writer, the text
// form included, rounds through float32 before it spells or stores, so the
// value only differs in memory, which is where this test looks.
func TestJsTableFloat32DefaultIsItsFloat32(t *testing.T) {
	t.Parallel()
	src := "package f32def\n\ntable Tab\n{\n    td float32 = 0.7\n    te float32 = -0.7 | min = -0.7, max = 0.7, resolution = 0.001\n    tf float64 = 0.7\n}\n"
	var mod string
	for name, data := range generateBoth(t, src) {
		if strings.HasSuffix(name, ".js") && strings.Contains(string(data), "this.td = ") {
			mod = string(data)
		}
	}
	if mod == "" {
		t.Fatal("no generated module constructs Tab")
	}
	for _, want := range []string{
		"this.td = 0.699999988079071;",
		"this.te = -0.699999988079071;",
		"this.tf = 0.7;",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("the constructor does not say %q", want)
		}
	}
	for field, want := range map[string]string{"td": "0.699999988079071", "te": "-0.699999988079071", "tf": "0.7"} {
		re := regexp.MustCompile(`\{ n: "` + field + `", key: "` + field + `",[^\n]*def: ([^ ,}]+) \}`)
		m := re.FindStringSubmatch(mod)
		if m == nil {
			t.Errorf("no descriptor for %s", field)
			continue
		}
		if m[1] != want {
			t.Errorf("the descriptor of %s says def: %s, want %s", field, m[1], want)
		}
	}
}
