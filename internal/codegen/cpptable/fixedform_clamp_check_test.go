package cpptable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// THE CLAMP CHECK IS THE CLAMP BODY WITH ITS STORES TAKEN OUT. The root runs a
// type's *FixedClampBody only when its *FixedInRange says some clamp would
// fire, so a check that misses one of the body's counts would let an
// out-of-range value through uncounted. This pins, over every committed C++
// table golden, that each check (1) stores nothing — no assignment but its own
// `bad` — (2) carries exactly one `bad |=` per count site of its body (a
// `clamped +=`, a `clamped++` or a `damaged++`), and that (3) every root calls
// its body only behind its check.
func TestFixedClampCheckMirrorsTheBody(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "golden", "tables", "*", "*Table.h"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no table goldens found: %v", err)
	}
	check := regexp.MustCompile(`(?s)TABLE_FIXED_INLINE bool (\w+)FixedInRange\( const \w+ & value \)\n\{\n(.*?)\n\}\n`)
	body := regexp.MustCompile(`(?s)\ninline void (\w+)FixedClampBody\( \w+ & value, [^)]*\)\n\{\n(.*?)\n\}\n`)
	root := regexp.MustCompile(`(?m)^    (.*?)(\w+)FixedClampBody\( value, clamped, damaged \);`)
	store := regexp.MustCompile(`[^=!<>|&+\-*/]=[^=]`)
	types := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		checks := map[string]string{}
		for _, m := range check.FindAllStringSubmatch(src, -1) {
			checks[m[1]] = m[2]
		}
		for _, m := range body.FindAllStringSubmatch(src, -1) {
			name, b := m[1], m[2]
			c, ok := checks[name]
			if !ok {
				t.Errorf("%s: %sFixedClampBody has no %sFixedInRange", f, name, name)
				continue
			}
			types++
			counts := strings.Count(b, "clamped +=") + strings.Count(b, "clamped++") + strings.Count(b, "damaged++") +
				strings.Count(b, "FixedClampBody(")
			ors := strings.Count(c, "bad |=")
			if counts != ors {
				t.Errorf("%s: %s: the body has %d count sites and the check %d `bad |=`", f, name, counts, ors)
			}
			for _, line := range strings.Split(c, "\n") {
				l := strings.TrimSpace(line)
				if l == "int bad = 0;" || strings.HasPrefix(l, "for (") || strings.HasPrefix(l, "//") {
					continue
				}
				l = strings.TrimPrefix(l, "bad |=")
				if store.MatchString(" " + l + " ") {
					t.Errorf("%s: %sFixedInRange stores: %q", f, name, l)
				}
			}
		}
		for _, m := range root.FindAllStringSubmatch(src, -1) {
			if m[1] != "if ( !"+m[2]+"FixedInRange( value ) ) { " {
				t.Errorf("%s: the root calls %sFixedClampBody without its check", f, m[2])
			}
		}
	}
	if types == 0 {
		t.Fatal("no clamp body found in any golden: the pattern no longer matches the emitter")
	}
}
