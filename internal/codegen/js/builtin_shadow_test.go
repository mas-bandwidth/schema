package js

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A schema type may be named like a JavaScript builtin the generated code
// calls. The class then takes that name in the module's scope, and every call
// the code meant for the builtin must still reach the builtin: the generator
// spells every builtin it calls through globalThis, in every packet module.
const builtinShadowSchema = `package clif

type Number { x float32 }
type Math { y float64 }
type Map { z uint64 }
type Set { w int32 }

type Probe
{
    n Number
    m Math
    p Map
    s Set
    f float32
    g float64
    c float32 | min = 0, max = 1, resolution = 0.01
    u uint64
}
`

func generateBuiltinShadow(t *testing.T) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(builtinShadowSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(builtinShadowSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// bareBuiltin finds a builtin called by its plain name on a code line — the
// spelling a same-named schema class captures.
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

func TestJsBuiltinsSurviveSameNamedSchemaTypes(t *testing.T) {
	t.Parallel()
	out := generateBuiltinShadow(t)
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
			if shadowedCall(text, code, "Number", "Math") {
				t.Errorf("%s:%d: a builtin the module's own class shadows is called by its plain name: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
	if !strings.Contains(string(out["Probe.js"]), "globalThis.Number") || !strings.Contains(string(out["Probe.js"]), "globalThis.Math") {
		t.Errorf("Probe.js declares Number and Math and must reach the builtins through globalThis:\n%s", out["Probe.js"])
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node and the serialize.js runtime (it runs the generated modules)")
		node := jsFlatNode(t)
		index := serializeJSIndex(t)
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
		harness := `import { pathToFileURL } from 'node:url';
import { Probe, WriteProbe, ReadProbe } from './Probe.js';
const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);
` + strings.Join(imports, "\n") + `

const value = new Probe();
value.f = 1.5;
value.g = -2.25;
value.c = 0.5;
const ws = new WriteStream(new Uint8Array(256));
if (!WriteProbe(ws, value)) {
  throw new Error('the writer refused a default packet');
}
ws.flush();
const back = new Probe();
if (!ReadProbe(new ReadStream(ws.data()), back)) {
  throw new Error('the reader refused what the writer wrote');
}
if (back.f !== 1.5 || back.g !== -2.25 || back.n.x !== 0 || back.m.y !== 0) {
  throw new Error('round trip moved values');
}
console.log('OK');
`
		runWideRangeHarness(t, node, dir, "shadow", harness, index, false)
	})
}
