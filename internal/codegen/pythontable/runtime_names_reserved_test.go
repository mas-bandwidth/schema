package pythontable

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A broad unit: variable and fixed tables, a table union, an enum, flags, a
// const, a packet type and every default surface, so the generated modules
// spell every runtime import they can.
const runtimeNamesSchema = `package namescan
const limit uint8 = 3
enum Mode { off, on }
flags Caps { jump, crouch }
type Point { x int32 }
fixed table Inner { v int32 }
union Choice { nested Inner
 count int32 }
fixed table Msg { a [2..4]int32
 c Choice
 f float32 | min = 0, max = 1, resolution = 0.01 }
table Root { mode Mode
 caps Caps
 name string(8)
 data bytes(4)
 items [..4]Choice
 next *Root
 point Point
 inner Inner }
`

var importLine = regexp.MustCompile(`(?m)^from (\S+) import (.+)$`)

// unprefixedRuntimeNames is every name a generated module or the package's
// __init__.py binds at module scope by importing it from the runtime, typing
// or builtins without the generated code's own prefix: the names a
// declaration spelled the same way would shadow.
func unprefixedRuntimeNames(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	seen := map[string]bool{}
	for path, content := range files {
		// __init__.py is not scanned: there a schema export keeps its public
		// binding by rule (packageInit skips the runtime name, or the
		// declaration's later import replaces it, and the runtime keeps its
		// own reference), which TestPythonMessagePublicNameCollision pins.
		if !strings.HasSuffix(path, ".py") || strings.Contains(path, "/_schema/") || strings.HasSuffix(path, "__init__.py") {
			continue
		}
		for _, m := range importLine.FindAllStringSubmatch(string(content), -1) {
			from := m[1]
			if from == "__future__" || from == "." || (strings.HasPrefix(from, ".") && !strings.HasPrefix(from, "._schema")) {
				continue // a sibling module's declarations, or a module itself
			}
			for item := range strings.SplitSeq(strings.Trim(m[2], "()"), ",") {
				fields := strings.Fields(item)
				if len(fields) == 0 {
					continue
				}
				name := fields[len(fields)-1]
				if !strings.HasPrefix(name, "_") {
					seen[name] = true
				}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// admitted checks src with the front end and reports the unit when it is legal.
// The emitter name rules (SPEC §4.6) stay out: check.Unit refuses a runtime
// import spelling for every target already, and this pins that Generate
// refuses it by name on its own, as a unit built in memory meets it.
func admitted(src string) (*ir.Unit, bool) {
	ast, perrs := parser.Parse("Scan.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, false
	}
	u, errs := check.UnitWithoutEmitterRules([]check.SourceFile{{Path: "Scan.schema", Name: "Scan.schema", Base: "Scan", Bytes: []byte(src), AST: ast}})
	return u, len(errs) == 0
}

// PYTHON-3: every runtime name a generated module imports unprefixed is
// reserved for a unit that declares a table, so no admitted declaration can
// shadow it: Generate refuses the declaration by name rather than emitting a
// module whose own class replaces the runtime binding its code calls.
func TestPythonRuntimeImportsAreReserved(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, runtimeNamesSchema))
	if err != nil {
		t.Fatal(err)
	}
	names := unprefixedRuntimeNames(t, files)
	for _, want := range []string{"FieldDesc", "register", "save_table", "Refused", "LoadReport"} {
		if !strings.Contains(" "+strings.Join(names, " ")+" ", " "+want+" ") {
			t.Fatalf("the scan lost %s; it found %v", want, names)
		}
	}
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}
	for _, name := range tableModuleImports {
		if !found[name] {
			t.Errorf("tableModuleImports lists %s, which no generated module imports unprefixed any more; drop it", name)
		}
	}
	tried := 0
	for _, name := range names {
		for _, decl := range []string{
			"table " + name + " { x int32 }\n",
			"enum " + name + " { a, b }\ntable Root { x int32 }\n",
			"const " + name + " uint8 = 1\ntable Root { x int32 }\n",
		} {
			u, ok := admitted("package namescan\n" + decl)
			if !ok {
				continue // the front end refuses the spelling for every target
			}
			tried++
			if _, err := Generate(u); err == nil {
				t.Errorf("%s: a declaration spelled like the runtime import %s generated; it must be refused by name\n%s", name, name, decl)
			} else if !strings.Contains(err.Error(), name) {
				t.Errorf("%s: refused without naming it: %v", name, err)
			}
		}
	}
	if tried == 0 {
		t.Fatalf("no runtime import spelling reached Generate; the probe declarations are wrong")
	}
}
