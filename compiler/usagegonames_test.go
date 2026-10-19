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
	checkedUnits := map[string]bool{}
	checkedNames := 0
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
		checkedUnits[name] = true
		declared := usageGoDeclaredNames(t, f.body, currentSchema, name)
		calls, types := usageGoTableNames(t, f.body, name)
		checkedNames += len(calls) + len(types)
		for id := range calls {
			if kind := declared[id]; kind != "func" && kind != "type" {
				t.Errorf("%s line %d: %s is called but generated Go declares no func or type of that name", "../docs/USAGE.md", f.line, id)
			}
		}
		for id := range types {
			if declared[id] != "type" {
				t.Errorf("%s line %d: %s is used as a type but generated Go declares no type of that name", "../docs/USAGE.md", f.line, id)
			}
		}
	}
	if len(checkedUnits) == 0 {
		t.Fatal(`no Go block in docs/USAGE.md's "Tables from Go" follows a schema unit — the gate would pass without checking a name`)
	}
	if checkedNames == 0 {
		t.Fatalf("checked %v but found no table-prefixed name in any of their Go blocks", checkedUnits)
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
// kind of each top-level func and type the output declares.
func usageGoDeclaredNames(t *testing.T, goBlock, schema, table string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	files, err := New().Generate(unitFromSource(t, schema), "go", Options{})
	if err != nil {
		t.Fatalf("generate go for %s at doc line: %v", table, err)
	}
	declared := map[string]string{}
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
				declared[d.Name.Name] = "func"
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if s, ok := spec.(*ast.TypeSpec); ok {
						declared[s.Name.Name] = "type"
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

// usageGoTableNames finds table-prefixed identifiers used as calls or types
// in a Go recipe. Value references, such as a generated fixed plan, are not
// API names that the recipe calls.
func usageGoTableNames(t *testing.T, goBlock, table string) (map[string]bool, map[string]bool) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "usage.go", goBlock, 0)
	if err != nil {
		t.Fatalf("parse Go usage block: %v", err)
	}
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(table) + `[A-Z]\w*$`)
	calls, types := map[string]bool{}, map[string]bool{}
	addType := func(expr ast.Expr) {
		if expr == nil {
			return
		}
		ast.Inspect(expr, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && re.MatchString(id.Name) {
				types[id.Name] = true
			}
			return true
		})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && re.MatchString(id.Name) {
				calls[id.Name] = true
			}
			if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "make" || id.Name == "new") && len(n.Args) > 0 {
				addType(n.Args[0])
			}
		case *ast.ValueSpec:
			addType(n.Type)
		case *ast.Field:
			addType(n.Type)
		case *ast.CompositeLit:
			addType(n.Type)
		}
		return true
	})
	return calls, types
}
