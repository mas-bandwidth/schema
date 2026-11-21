// AN INTEGER LITERAL REACHES GENERATED CODE IN A SPELLING EVERY TARGET READS
// AS THE COMPILER'S VALUE (SPEC §4.1). The packet backends render a literal
// bound symbolically, so the source text used to land in the code verbatim:
// `0b1010` is no C99 literal, `0X1F` is no Rust literal, and a leading-zero
// decimal (`010`, refused by the scanner now) was octal in C, C++ and Go.
// This test generates each verbatim-rendering leg from one tiny declaration
// and holds the code part of every line (comments stripped) to hex with a
// lowercase `0x` or decimal. No toolchain runs: it reads the emitted text.
package compiler

import (
	"regexp"
	"strings"
	"testing"
)

const literalSpellingSource = `package lit
const B = 0b1010
const H = 0X1F
type P
{
    name string(0b1010)
    tag string(0X1F)
    n uint8 | min = 0, max = 0B11
    k [..0b11]uint8
}
`

// badSpelling is a literal some target reads differently or cannot parse:
// a binary prefix in either case, or an uppercase hex prefix.
var badSpelling = regexp.MustCompile(`(^|[^0-9A-Za-z_])0([bB][01]|X[0-9A-Fa-f])`)

// codePart drops a line's comment, so the schema spelling carried in a
// comment beside a folded value is not counted.
func codePart(line string) string {
	for _, marker := range []string{"//", "/*"} {
		if i := strings.Index(line, marker); i >= 0 {
			line = line[:i]
		}
	}
	return line
}

func TestGeneratedIntegerLiteralSpelling(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, literalSpellingSource)
	want := map[string][]string{
		"c":    {"char name[10 + 1];", "char tag[0x1F + 1];"},
		"cpp":  {"char name[10 + 1]", "char tag[0x1F + 1]"},
		"go":   {"[10]byte", "[0x1F]byte"},
		"js":   {"new Uint8Array(10)", "new Uint8Array(0x1F)"},
		"rust": {"[u8; 10]", "[u8; 0x1F]", "= 0x1F;"},
		"cs":   {"SerializeInt(ref value.NameLength, 0, 10)", "SerializeInt(ref rangeValue, 0, 3)"},
	}
	for lang, needles := range want {
		files, err := New().Generate(u, lang, Options{})
		if err != nil {
			t.Fatalf("%s: generate: %v", lang, err)
		}
		var all strings.Builder
		for name, body := range files {
			for i, line := range strings.Split(string(body), "\n") {
				if badSpelling.MatchString(codePart(line)) {
					t.Errorf("%s: %s:%d carries a literal spelling the target reads differently: %s", lang, name, i+1, strings.TrimSpace(line))
				}
			}
			all.Write(body)
		}
		for _, needle := range needles {
			if !strings.Contains(all.String(), needle) {
				t.Errorf("%s: generated code lacks %q", lang, needle)
			}
		}
	}
}
