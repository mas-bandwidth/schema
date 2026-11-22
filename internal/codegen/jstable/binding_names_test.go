package jstable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A declaration named eval, arguments or undefined cannot be a module-scope
// binding in the generated JavaScript, and one named after a TypeScript
// predefined type cannot be declared in the .d.ts, so the checker refuses it before any
// module is written (internal/check, SPEC §4.6). The same words as FIELDS are
// properties: the unit generates, every module loads, and the values survive
// a save and a load.
func TestJsFieldsNamedLikeUnbindableNamesRoundTrip(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"eval", "arguments", "undefined", "unknown", "never", "symbol", "number", "bigint"} {
		src := "package demo\n\ntype " + name + "\n{\n    value uint32\n}\n\ntable Entry\n{\n    part " + name + "\n}\n"
		ast, perrs := parser.Parse("Demo.schema", []byte(src))
		if len(perrs) != 0 {
			t.Fatalf("parse: %v", perrs)
		}
		_, cerrs := check.Unit([]check.SourceFile{{
			Path: "Demo.schema", Name: "Demo.schema", Base: "Demo", Bytes: []byte(src), AST: ast,
		}})
		if len(cerrs) == 0 {
			t.Errorf("type %s reached the JavaScript generator: its module cannot bind that name", name)
		}
	}

	u := unitFrom(t, `package demo

type Clean
{
    value uint32
}

type intrinsic
{
    value uint32
}

type declare
{
    value uint32
}

table Entry
{
    part Clean
    a intrinsic
    b declare
    eval uint32
    arguments uint32
    undefined uint32
    unknown uint32
    never uint32
    symbol uint32
    number uint32
    bigint uint32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it saves and loads the generated table)")
		out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsBindingNamesDriver, struct{}{})
		if !strings.Contains(out, "BINDING NAMES OK\n") {
			t.Fatalf("the fields did not round-trip: %s", out)
		}
	})
}

const jsBindingNamesDriver = `
import {pathToFileURL} from "node:url";
const v = await import(pathToFileURL(process.argv[3]).href);
const expect = (ok, msg) => { if (!ok) { throw new Error(msg); } };
const e = new v.Entry();
e.part.value = 42;
e.eval = 21;
e.arguments = 42;
e.undefined = 7;
e.a.value = 1; e.b.value = 2;
e.unknown = 3; e.never = 4; e.symbol = 5; e.number = 6; e.bigint = 8;
const wire = v.EntrySave(e);
expect(wire !== null && wire.length > 0, "save");
const report = new v.TableVariableReport();
const back = v.EntryLoad(wire, report);
expect(back !== null && !report.damaged, "load");
expect(back.part.value === 42, "part.value " + back.part.value);
expect(back.eval === 21, "eval " + back.eval);
expect(back.arguments === 42, "arguments " + back.arguments);
expect(back.undefined === 7, "undefined " + back.undefined);
expect(back.a.value === 1 && back.b.value === 2, "intrinsic and declare");
expect(back.unknown === 3 && back.never === 4 && back.symbol === 5 && back.number === 6 && back.bigint === 8, "the fields named like TypeScript types");
console.log("BINDING NAMES OK");
`
