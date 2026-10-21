package darttable

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE DART FIXED LIBRARIES PASS `dart analyze` (gates/dart-fixed-analyze,
// issue 547 Dart leg). The gate is exact: the emitter must say only what the
// body uses, so an import's `show` list never names a member the library does
// not call (unused_shown_name), and no `final n =` local is declared without a
// later read of n in its block (unused_local_variable). This test is pure Go
// over the generated text — no Dart SDK, no sockets, no outside service — so
// it is a unit test and runs in `go test ./internal/codegen/darttable/`.
func TestFixedLibrariesShowAndDeclareOnlyWhatTheyUse(t *testing.T) {
	t.Parallel()
	corpora := []struct {
		name string
		u    *ir.Unit
	}{
		{"examples", loadDirUnit(t, "../../../tables/examples")},
		{"bench", loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")},
		{"fx1", loadUnit(t, "../../../test/tables/FX1.schema")},
		{"p1", loadUnit(t, "../../../test/tables/P1.schema")},
		{"v1", loadUnit(t, "../../../test/tables/V1.schema")},
	}
	for _, c := range corpora {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(c.u)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			names := make([]string, 0, len(files))
			for n := range files {
				if strings.HasSuffix(n, ".dart") {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			for _, n := range names {
				n := n
				t.Run(n, func(t *testing.T) {
					t.Parallel()
					src := string(files[n])
					assertNoUnusedShownName(t, src, n)
					assertNoUnusedFinalN(t, src, n)
				})
			}
		})
	}
}

// importStmt matches one whole import statement, the flat form and the
// formatter's tall forms, through its terminating `;`.
var importStmt = regexp.MustCompile(`(?s)import\s+'[^']*'(?:\s+show\s+.*?)?;`)

// importShow matches an import that carries a `show` list and captures the list.
var importShow = regexp.MustCompile(`(?s)import\s+'[^']*'\s+show\s+(.*?);`)

// shownNames answers every name an import's `show` list names.
func shownNames(src string) []string {
	var out []string
	for _, m := range importShow.FindAllStringSubmatch(src, -1) {
		for _, s := range strings.Split(m[1], ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// stripImports removes every import statement, leaving the library body.
func stripImports(src string) string {
	return importStmt.ReplaceAllString(src, "")
}

// assertNoUnusedShownName holds the analyzer's unused_shown_name over one
// library: a name an import's `show` list names must occur in the library
// outside the import.
func assertNoUnusedShownName(t *testing.T, src, lib string) {
	t.Helper()
	body := stripImports(src)
	for _, s := range shownNames(src) {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(s) + `\b`)
		if !re.MatchString(body) {
			t.Errorf("%s: imports %s from a sibling with `show` but never uses it (unused_shown_name)", lib, s)
		}
	}
}

const declFinalN = `\bfinal n =`

var finalNRe = regexp.MustCompile(declFinalN)
var wordNRe = regexp.MustCompile(`\bn\b`)

// isIdent reports whether a byte can sit inside a Dart identifier.
func isIdent(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// braceDepth counts unclosed `{` at a byte offset.
func braceDepth(src string, pos int) int {
	d := 0
	for i := 0; i < pos && i < len(src); i++ {
		switch src[i] {
		case '{':
			d++
		case '}':
			d--
		}
	}
	return d
}

// assertNoUnusedFinalN holds the analyzer's unused_local_variable over one
// library in the one shape this leg's message reader had: a `final n =` local
// must be read again in its block.
func assertNoUnusedFinalN(t *testing.T, src, lib string) {
	t.Helper()
	for _, m := range finalNRe.FindAllStringIndex(src, -1) {
		start := braceDepth(src, m[0])
		used := false
		d := start
		for j := m[1]; j < len(src); j++ {
			switch src[j] {
			case '{':
				d++
			case '}':
				d--
				if d < start {
					goto closed
				}
			}
			if !used && src[j] == 'n' &&
				(j == 0 || !isIdent(src[j-1])) && (j+1 >= len(src) || !isIdent(src[j+1])) {
				used = true
			}
		}
	closed:
		if !used {
			t.Errorf("%s: `final n =` declares n but never reads it in its block (unused_local_variable)", lib)
		}
	}
}

// loadDirUnit builds a unit from every *.schema in a directory, the way the
// fixed form's `loadUnit` does for named files.
func loadDirUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(e.Name(), ".schema")
		ast, errs := parser.Parse(e.Name(), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", e.Name(), errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: e.Name(), Base: base, Bytes: src, AST: ast})
	}
	if len(files) == 0 {
		t.Fatalf("no .schema files in %s", dir)
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}
