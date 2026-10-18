// TestUsageGoNamesAreEmitted: the Go blocks of docs/USAGE.md's "Tables from
// Go" section are complete test bodies that run against a compiled unit, so
// every identifier of the form <Table><Suffix> a block uses must be a name the
// generated Go actually declares. A block that names a func or type the
// emitter does not emit fails with the block's line and the name.
package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestUsageGoNamesAreEmitted(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile("../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	type fence struct {
		lang string
		body string
		line int
	}
	var fences []fence
	lines := strings.Split(string(page), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "### Tables from Go" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatal(`docs/USAGE.md has no "Tables from Go" section`)
	}
	inside := false
	var lang string
	var body []string
	bodyStart := 0
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			if inside {
				fences = append(fences, fence{lang: lang, body: strings.Join(body, "\n") + "\n", line: bodyStart})
			}
			inside, lang, body = !inside, strings.TrimPrefix(line, "```"), nil
			bodyStart = i + 2
			continue
		}
		if !inside && strings.HasPrefix(line, "#") {
			break
		}
		if inside {
			body = append(body, line)
		}
	}

	currentSchema := ""
	for _, f := range fences {
		if usageFenceIsSchema(f.body) {
			currentSchema = f.body
			continue
		}
		if f.lang != "go" || currentSchema == "" {
			continue
		}
		name := usageTableName(currentSchema)
		if name == "" {
			continue
		}
		declared := usageGoDeclaredNames(t, f.body, currentSchema, name)
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `[A-Z]\w*`)
		for _, id := range re.FindAllString(f.body, -1) {
			if !declared[id] {
				t.Errorf("%s line %d: %s is called but the generated Go declares no func or type of that name", "../docs/USAGE.md", f.line, id)
			}
		}
	}
}

// usageFenceIsSchema is whether a fenced block is a schema declaration.
func usageFenceIsSchema(body string) bool {
	hasPackage, hasTable := false, false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		hasPackage = hasPackage || strings.HasPrefix(trimmed, "package ")
		hasTable = hasTable || strings.HasPrefix(trimmed, "table ") || strings.HasPrefix(trimmed, "fixed table ")
	}
	return hasPackage && hasTable
}

// usageTableName is the first table name a schema unit declares.
func usageTableName(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"table ", "fixed table "} {
			if rest, ok := strings.CutPrefix(trimmed, prefix); ok {
				if name, _, ok := strings.Cut(rest, " "); ok && name != "" {
					return name
				}
				return rest
			}
		}
	}
	return ""
}

// usageGoDeclaredNames generates the unit's Go in a temp dir and returns the
// set of every top-level func, type, var and const the output declares.
func usageGoDeclaredNames(t *testing.T, goBlock, schema, table string) map[string]bool {
	t.Helper()
	dir := t.TempDir()
	files, err := New().Generate(unitFromSource(t, schema), "go", Options{})
	if err != nil {
		t.Fatalf("generate go for %s at doc line: %v", table, err)
	}
	declared := map[string]bool{}
	for name, content := range files {
		if err := os.WriteFile(dir+"/"+name, content, 0o644); err != nil {
			t.Fatalf("write generated %s: %v", name, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, content, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				declared[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							declared[n.Name] = true
						}
					}
				}
			}
		}
	}
	if len(files) == 0 {
		t.Fatalf("%s: template unit generated no Go files", table)
	}
	return declared
}
