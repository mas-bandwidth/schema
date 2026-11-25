package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// moveLayer gives every unmoved test counted on one layer's ledger lines its
// registry row, appended to the registry in package and name order, and
// returns how many it moved. Only the kinds the driver has an adapter for
// move: a language-free Go test becomes a gotest row. A test goTestLayer
// gives a later layer (a benchmark, a test in a module of its own) stays in
// the ledger. Its tier is its tier line's (extract.ApplyTiers), else unit;
// a key two tests would share, a
// family two packages would share, and a layer with nothing to move are
// refused by name.
func moveLayer(root, regPath, layer string, res *extract.Result, reg *testreg.Registry, ledger []extract.LedgerLine) (int, error) {
	places := map[string]bool{}
	for _, l := range ledger {
		if l.Layer != layer || l.Kind == extract.KindTier {
			// a tier line is a fact about a test a count line counts: the
			// test moves with its count line, and its row takes the tier
			continue
		}
		if l.Kind != extract.KindGoTest {
			return 0, fmt.Errorf("%s:%d: %s %s is %s; --move has no adapter for that kind yet", extract.LedgerPath, l.Line, l.Kind, l.Dir, l.Kind)
		}
		places[l.Dir] = true
	}
	if len(places) == 0 {
		return 0, fmt.Errorf("%s has no line of layer %q", extract.LedgerPath, layer)
	}
	covered, problems := extract.Covered(reg)
	if len(problems) > 0 {
		return 0, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	familyPkg := map[string]string{} // family -> the package whose rows use it
	keys := map[string]bool{}
	for _, t := range reg.Tests {
		keys[t.Key] = true
		if t.Adapter == "gotest" {
			var in struct{ Package string }
			if err := json.Unmarshal(t.Inputs, &in); err == nil {
				familyPkg[t.Family] = strings.TrimPrefix(in.Package, "./")
			}
		}
	}
	pkgFamily := map[string]string{}
	for f, p := range familyPkg {
		pkgFamily[p] = f
	}
	var b bytes.Buffer
	n := 0
	for _, it := range res.Items {
		if it.Kind != extract.KindGoTest || !places[it.Dir] {
			continue
		}
		if l, _ := goTestLayer(root, it); l != "L3" {
			continue
		}
		if _, ok := covered[it.ID()]; ok {
			continue
		}
		family, ok := pkgFamily[it.Dir]
		if !ok {
			family = familyOf(it.Dir)
			if other, taken := familyPkg[family]; taken {
				return 0, fmt.Errorf("package %s and package %s both give family %q", it.Dir, other, family)
			}
			familyPkg[family], pkgFamily[it.Dir] = it.Dir, family
		}
		typ := typeOf(it.Dir, it.Name)
		c := caseOf(it.Name)
		key := typ + "." + family + "." + c
		if keys[key] {
			return 0, fmt.Errorf("%s %s gives key %s, which another test has", it.Dir, it.Name, key)
		}
		keys[key] = true
		line, err := goTestRow(key, typ, family, c, it)
		if err != nil {
			return 0, err
		}
		b.WriteString(line)
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("layer %s's ledger lines count no test --move can move", layer)
	}
	f, err := os.OpenFile(filepath.Join(root, regPath), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		_ = f.Close() // errdiscard: the write failed, and its error is the one returned
		return 0, err
	}
	return n, f.Close()
}

// goTestRow is a language-free test's registry line, its fields in the
// order the registry's other gotest rows give them.
func goTestRow(key, typ, family, c string, it extract.Item) (string, error) {
	pkg := "./" + it.Dir
	fields := []struct {
		name  string
		value any
	}{
		{"rec", "test"}, {"key", key}, {"type", typ}, {"family", family}, {"case", c}, {"tier", it.Tier},
		{"scope", testreg.LangFree}, {"adapter", "gotest"},
		{"checks", "Go test " + it.Name + " in " + pkg},
		{"inputs", goTestInputs{Package: pkg, Test: it.Name}},
		{"expect", "the Go test passes"},
		{"source", it.Source},
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, f := range fields {
		v, err := marshal(f.value)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%s", f.name, v)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// corpusFamilyPrefix begins the family of every conformance corpus row, and
// only theirs (test/conformance/harness/adapter.go's corpusFamilyPrefix):
// TestTheRegistryHoldsEveryConformanceCase reads every row of such a family
// as a corpus row.
const corpusFamilyPrefix = "conformance-"

// familyOf is a package's family: its directory without the leading
// internal/, tools/, test/conformance/ or test/, slashes made dashes. So
// test/conformance/harness is harness (the family its hand-written rows
// use), and no Go test family begins with the corpus rows' prefix.
func familyOf(dir string) string {
	for _, p := range []string{"internal/", "tools/", "test/conformance/", "test/"} {
		if rest, ok := strings.CutPrefix(dir, p); ok {
			dir = rest
			break
		}
	}
	return strings.ToLower(strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(dir))
}

// toolingPackages are the language-free packages that check the repository
// and its CI rather than the compiler: CI policy, docs, harnesses and the
// tools the workflows run.
var toolingPackages = regexp.MustCompile(`^(tools|test|editors|bench/tools)(/|$)|^internal/(ci|docgate|githooks|skipledger|slowtest|recipescan|nightlyrows|provesran|vacuous|runmatch|testproc|testinputs|testreg|modulemove|goldens|inputfamilies|memceiling|baseline)(/|$)`)

// typeOf is a language-free Go test's type: fuzz for a fuzz target, bench
// under bench/, tooling for the repository's own checks, and compiler for
// the compiler, its IR and its emitters' shared code.
func typeOf(dir, name string) string {
	switch {
	case strings.HasPrefix(name, "Fuzz"):
		return "fuzz"
	case toolingPackages.MatchString(dir):
		return "tooling"
	case strings.HasPrefix(dir, "bench/"):
		return "bench"
	}
	return "compiler"
}

// caseOf is a Go test's case: its name past "Test", in lowercase words
// joined by dashes (TestEveryDriverTestIsRegistered is
// every-driver-test-is-registered). A fuzz target keeps "fuzz" in front.
func caseOf(name string) string {
	rest, ok := strings.CutPrefix(name, "Test")
	if !ok {
		rest = name
	}
	rs := []rune(rest)
	var b strings.Builder
	for i, r := range rs {
		if r == '_' {
			b.WriteByte('-')
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	c := strings.Trim(regexp.MustCompile(`-+`).ReplaceAllString(b.String(), "-"), "-")
	if c == "" {
		c = "test"
	}
	return c
}
