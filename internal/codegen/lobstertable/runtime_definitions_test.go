package lobstertable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// retiredRuntimeNames are the text-number helpers and the text clamp helper
// the table runtime no longer carries. No runtime module may define one, and
// no other source may still call one: the text reader parses its integers
// through j_parse_wide and refuses ill-formed text instead of clamping it.
var retiredRuntimeNames = []string{
	"t_int", "i128_mul_small", "i128_div_small", "TextDec", "t_dec",
	"t_dec_value", "t_int_token", "t_fixed_token", "text_boundary",
	"i128_add", "i128_neg", "i128_bit", "i128_setbit",
}

var lobsterDefinition = regexp.MustCompile(`^(\s*)(def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
var lobsterWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

type lineSpan struct {
	start int
	end   int
	name  string
}

// TestLobsterRuntimeDefinitionsAreCalled holds the retired text-number helpers
// and the text clamp helper out of the runtime: each is gone, and no scanned
// source calls it. A name that is still defined, or is mentioned outside a
// comment and outside its own definition and body, fails.
func TestLobsterRuntimeDefinitionsAreCalled(t *testing.T) {
	t.Parallel()
	root := tableRepoRoot(t)
	type source struct {
		path     string
		contents string
		runtime  bool
	}
	var sources []source
	for name, data := range Runtime() {
		sources = append(sources, source{"runtime/" + name, string(data), true})
	}
	disk, err := runtimeScanFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range disk {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source{filepath.ToSlash(rel), string(data), false})
	}

	want := map[string]bool{}
	for _, name := range retiredRuntimeNames {
		want[name] = true
	}
	// own describes the line ranges a name's own definition and body occupy,
	// so a name mentioned there is not a caller.
	own := map[string]map[string][][2]int{}
	type mention struct {
		path string
		line int
		text string
	}
	mentions := map[string][]mention{}
	for _, src := range sources {
		code := stripSourceComments(src.path, src.contents)
		lines := strings.Split(code, "\n")
		if src.runtime {
			for _, span := range lobsterDefinitionSpans(lines) {
				if !want[span.name] {
					continue
				}
				if own[src.path] == nil {
					own[src.path] = map[string][][2]int{}
				}
				own[src.path][span.name] = append(own[src.path][span.name], [2]int{span.start, span.end})
				t.Errorf("%s is still defined at %s:%d", span.name, src.path, span.start+1)
			}
		}
		for i, line := range lines {
			for _, word := range lobsterWord.FindAllString(line, -1) {
				if !want[word] {
					continue
				}
				excluded := false
				for _, span := range own[src.path][word] {
					if i >= span[0] && i < span[1] {
						excluded = true
						break
					}
				}
				if !excluded {
					mentions[word] = append(mentions[word], mention{src.path, i + 1, strings.TrimSpace(line)})
				}
			}
		}
	}
	for _, name := range retiredRuntimeNames {
		for _, m := range mentions[name] {
			t.Errorf("%s is still called at %s:%d: %s", name, m.path, m.line, m.text)
		}
	}
}

// tableRepoRoot is the checkout root, from the package directory the test
// runs in, with the runtime files and the corpus beside it.
func tableRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// runtimeScanFiles is every source the retire rule reads: the non-test Go
// sources of the two table/lobster packages, every .lobster file under test/
// and bench/, and docs/PORT-LOBSTER.md.
func runtimeScanFiles(root string) ([]string, error) {
	var out []string
	for _, dir := range []string{
		filepath.Join(root, "internal", "codegen", "lobstertable"),
		filepath.Join(root, "internal", "codegen", "lobster"),
	} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, dir := range []string{filepath.Join(root, "test"), filepath.Join(root, "bench")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".lobster") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out = append(out, filepath.Join(root, "docs", "PORT-LOBSTER.md"))
	return out, nil
}

// stripSourceComments blanks comments while keeping every line and column in
// place. Lobster and Markdown use //; Go also has /* */.
func stripSourceComments(path, text string) string {
	lines := strings.Split(text, "\n")
	inBlock := false
	for i, line := range lines {
		var b strings.Builder
		for j := 0; j < len(line); {
			if inBlock {
				if strings.HasPrefix(line[j:], "*/") {
					inBlock = false
					j += 2
				} else {
					j++
				}
				continue
			}
			if strings.HasSuffix(path, ".go") && strings.HasPrefix(line[j:], "/*") {
				inBlock = true
				j += 2
				continue
			}
			if strings.HasPrefix(line[j:], "//") {
				break
			}
			b.WriteByte(line[j])
			j++
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// lobsterDefinitionSpans returns each namespace-scope def and class with its
// body's [start, end) line range (0-based, the definition line included).
func lobsterDefinitionSpans(lines []string) []lineSpan {
	var spans []lineSpan
	for i := 0; i < len(lines); i++ {
		m := lobsterDefinition.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		indent := len(m[1])
		end := i + 1
		for end < len(lines) {
			line := lines[end]
			if strings.TrimSpace(line) == "" {
				end++
				continue
			}
			if len(line)-len(strings.TrimLeft(line, " \t")) <= indent {
				break
			}
			end++
		}
		spans = append(spans, lineSpan{start: i, end: end, name: m[3]})
		i = end - 1
	}
	return spans
}
