package rusttable

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rustPlainFloatOperand finds the corpus's plain control `p` as a store's
// operand (v.p, value.p), and not `pick`.
var rustPlainFloatOperand = regexp.MustCompile(`\b(v|value)\.p\b`)

// TestRustCompressedFloatStoresAreGuarded is the fast twin of
// TestRustCompressedNegZeroCorpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json
// fixed-compressed-negzero): it reads the generated Rust for the corpus's
// Probe.schema rather than compiling it. Every float store of the variable
// writer, the fixed writer, the JSON writer and the cook writes a compressed
// float through the +0 normalisation, `(if x == 0.0 { 0.0 } else { x })`,
// which is plain code and so holds in every build, never a debug assert; the
// plain control `p` is stored as held, so its -0 rides.
func TestRustCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(rustNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	// each writer's float store, by the spelling only a float store has
	stores := map[string]string{
		"variable": ".to_bits().to_le_bytes()",
		"fixed":    "b[at..at + 4].copy_from_slice(&",
		"json":     "json_float(",
		"cook":     "w.put_f32(",
	}
	const normalised = "== 0.0 { 0.0 } else {"
	counts := map[string][2]int{}
	var names []string
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for i, line := range strings.Split(string(modules[name]), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "pub fn") {
				continue
			}
			for writer, spelling := range stores {
				if !strings.Contains(code, spelling) {
					continue
				}
				guarded := strings.Contains(code, normalised)
				plain := rustPlainFloatOperand.MatchString(code)
				c := counts[writer]
				switch {
				case guarded && plain:
					t.Errorf("%s:%d: the %s writer normalises the plain float's -0: %s", name, i+1, writer, code)
				case !guarded && !plain:
					t.Errorf("%s:%d: the %s writer stores a compressed float as held: %s", name, i+1, writer, code)
				case guarded:
					c[0]++
				default:
					c[1]++
				}
				counts[writer] = c
			}
		}
	}
	for writer := range stores {
		if c := counts[writer]; c[0] == 0 || c[1] == 0 {
			t.Errorf("the %s writer has %d normalised compressed stores and %d plain ones; want both", writer, c[0], c[1])
		}
	}
}
