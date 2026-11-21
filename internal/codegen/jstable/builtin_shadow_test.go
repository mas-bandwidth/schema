package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A table module imports the unit's types, so a type named like a builtin the
// table code calls (Number, Math, Map, Set) is bound in the table module's
// scope too. Every table module still reaches the builtins: none calls one by
// a plain name its own scope binds, and every module loads and links.
const tableBuiltinShadowSchema = `package clif

type Number { x float32 }
type Math { y float64 }
type Map { z int32 }
type Set { w int32 }

table Shelf
{
    a float32
    b float64
    n Number
    m Math
    p Map
    s Set
}
`

var stringLiteral = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)

// shadowedCall reports a code line calling, by its plain name, a builtin the
// module binds a class of the same name over — string
// literals aside, which only quote names.
// bindsName reports a module declaring or importing name at module scope.
func bindsName(module, name string) bool {
	return regexp.MustCompile(`(?m)^(?:export\s+)?class\s+`+name+`\b`).MatchString(module) ||
		regexp.MustCompile(`(?m)^import\s*\{[^}]*\b`+name+`\b[^}]*\}`).MatchString(module)
}

func shadowedCall(module, code string, names ...string) bool {
	code = stringLiteral.ReplaceAllString(code, `""`)
	for _, n := range names {
		if bindsName(module, n) && regexp.MustCompile(`(?:^|[^.$\w])`+n+`\.`).MatchString(code) {
			return true
		}
	}
	return false
}

// generateBoth runs the JS and JS-table generators over one schema source.
func generateBoth(t *testing.T, src string) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		out[name] = data
	}
	return out
}

// A json key is the user's text: the generated modules carry it as written,
// whatever it spells. A key reading like a qualified builtin appears exactly
// as often, and in the same quoted form, as a key reading like nothing.
func TestJsTableJsonKeySpellingABuiltinIsKeptVerbatim(t *testing.T) {
	t.Parallel()
	count := func(key string) int {
		src := "package clif\n\nfixed table Player\n{\n    count int32 | json = \"" + key + "\"\n    hp int32\n}\n"
		n := 0
		for name, data := range generateBoth(t, src) {
			if strings.HasSuffix(name, ".js") {
				n += strings.Count(string(data), `"`+key+`"`)
			}
		}
		return n
	}
	plain := count("zq_count")
	if plain == 0 {
		t.Fatal("the control key appears in no generated module")
	}
	if got := count("globalThis.Number"); got != plain {
		t.Errorf("json key \"globalThis.Number\" appears %d times as written, the control key %d", got, plain)
	}
}

func TestJsTableBuiltinsSurviveSameNamedSchemaTypes(t *testing.T) {
	t.Parallel()
	out := generateBoth(t, tableBuiltinShadowSchema)
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		text := string(out[name])
		for i, line := range strings.Split(text, "\n") {
			code := line
			if at := strings.Index(code, "//"); at >= 0 {
				code = code[:at]
			}
			if strings.HasPrefix(strings.TrimSpace(code), "*") {
				continue
			}
			if shadowedCall(text, code, "Number", "Math", "Map", "Set") {
				t.Errorf("%s:%d: a builtin the module's imported class shadows is called by its plain name: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it loads every generated module)")
		node := jsNode(t)
		dir := t.TempDir()
		var imports []string
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), out[name], 0o600); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".js") {
				imports = append(imports, "await import('./"+name+"');")
			}
		}
		script := filepath.Join(dir, "load.mjs")
		if err := os.WriteFile(script, []byte(strings.Join(imports, "\n")+"\nconsole.log('OK');\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script)
		cmd.Dir = dir
		got, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(got), "OK") {
			t.Fatalf("node: %v\n%s", err, got)
		}
	})
}
