package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsKnownGlobals is every name the JavaScript and TypeScript global scopes
// give the generated modules and declarations: the ECMAScript globals, the
// host globals the runtime tier could touch, and TypeScript's global types.
// A generated module or .d.ts reaches each of these only as globalThis.X, so a
// schema type of the same name, a class in that module's scope, shadows none.
var jsKnownGlobals = strings.Fields(`
	AggregateError Array ArrayBuffer Atomics BigInt BigInt64Array BigUint64Array Boolean
	DataView Date Error EvalError FinalizationRegistry Float16Array Float32Array Float64Array
	Function Infinity Int8Array Int16Array Int32Array Intl Iterator JSON Map Math NaN Number
	Object Promise Proxy RangeError ReferenceError Reflect RegExp Set SharedArrayBuffer String
	Symbol SyntaxError TypeError URIError Uint8Array Uint8ClampedArray Uint16Array Uint32Array
	WeakMap WeakRef WeakSet isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent
	encodeURI encodeURIComponent
	TextEncoder TextDecoder console process Buffer queueMicrotask structuredClone setTimeout
	clearTimeout performance URL
	Readonly ReadonlyArray ReadonlyMap ReadonlySet Record Partial Required Pick Omit Exclude
	Extract NonNullable ReturnType Parameters InstanceType ConstructorParameters Awaited
	Iterable IterableIterator ArrayLike PromiseLike ArrayBufferLike ArrayBufferView PropertyKey
	Generator Uppercase Lowercase Capitalize Uncapitalize ThisType
`)

// jsGlobalCorpora are the units whose generated JavaScript the scan reads.
func jsGlobalCorpora(t *testing.T) []string {
	t.Helper()
	dirs := []string{"../examples", "../examples128"}
	entries, err := os.ReadDir("../tables")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join("../tables", e.Name()))
		}
	}
	return dirs
}

// jsGlobalUse is one global the scan met: where, and whether qualified.
type jsGlobalUse struct {
	name, at  string
	qualified bool
}

// scanJsGlobals lexes one generated JavaScript or TypeScript file and returns
// every identifier in known it meets in code: comments and string text are
// skipped (a template literal's ${...} is code), a property access (x.Name)
// and an object or declaration key (Name: ...) name no global, and
// globalThis.Name is the qualified spelling.
func scanJsGlobals(file, text string, known map[string]bool) []jsGlobalUse {
	var uses []jsGlobalUse
	line := 1
	i := 0
	n := len(text)
	// templateDepth counts open ${ inside template literals, braceDepth the
	// braces opened inside the innermost one
	var templates []int
	prevSig := byte('\n') // last significant code byte
	var scanTemplate func()
	scanString := func(q byte) {
		i++
		for i < n && text[i] != q {
			switch text[i] {
			case '\\':
				i++
			case '\n':
				line++
			}
			i++
		}
		i++
	}
	scanTemplate = func() {
		i++ // opening backquote or closing brace of ${...}
		for i < n {
			switch c := text[i]; {
			case c == '\\':
				i += 2
			case c == '`':
				i++
				return
			case c == '$' && i+1 < n && text[i+1] == '{':
				i += 2
				templates = append(templates, 0)
				return
			default:
				if c == '\n' {
					line++
				}
				i++
			}
		}
	}
	isIdent := func(c byte) bool {
		return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i < n {
		c := text[i]
		switch {
		case c == '\n':
			line++
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case c == '/' && i+1 < n && text[i+1] == '/':
			for i < n && text[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < n && text[i+1] == '*':
			i += 2
			for i+1 < n && (text[i] != '*' || text[i+1] != '/') {
				if text[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
			continue
		case c == '"' || c == '\'':
			scanString(c)
			prevSig = '"'
			continue
		case c == '`':
			scanTemplate()
			prevSig = '"'
			continue
		case c == '{' && len(templates) > 0:
			templates[len(templates)-1]++
		case c == '}' && len(templates) > 0:
			if templates[len(templates)-1] == 0 {
				templates = templates[:len(templates)-1]
				i-- // scanTemplate steps past the closing brace
				scanTemplate()
				prevSig = '"'
				continue
			}
			templates[len(templates)-1]--
		case isIdent(c) && (c < '0' || c > '9'):
			start := i
			for i < n && isIdent(text[i]) {
				i++
			}
			word := text[start:i]
			j := i
			for j < n && (text[j] == ' ' || text[j] == '\t') {
				j++
			}
			key := j < n && text[j] == ':' && (j+1 >= n || text[j+1] != ':') &&
				(prevSig == '{' || prevSig == ',' || prevSig == '\n' || prevSig == ';' || prevSig == '(')
			optKey := j+1 < n && text[j] == '?' && text[j+1] == ':'
			member := prevSig == '.'
			if known[word] && !key && !optKey {
				if member {
					// globalThis.X is the qualified spelling; any other
					// x.X is a property named like a global
					if k := start - 1; k >= len("globalThis") && strings.HasSuffix(strings.TrimRight(text[:k], " \t"), "globalThis") {
						uses = append(uses, jsGlobalUse{word, fmt.Sprintf("%s:%d", file, line), true})
					}
				} else {
					uses = append(uses, jsGlobalUse{word, fmt.Sprintf("%s:%d", file, line), false})
				}
			}
			prevSig = 'a'
			continue
		}
		prevSig = c
		i++
	}
	return uses
}

// declaredNames is every name a unit puts in its modules' scope.
func declaredNames(u *ir.Unit) map[string]bool {
	out := map[string]bool{}
	for _, s := range u.Structs {
		out[s.Name] = true
	}
	for _, e := range u.Enums {
		out[e.Name] = true
	}
	for _, f := range u.Flags {
		out[f.Name] = true
	}
	for _, un := range u.Unions {
		out[un.Name] = true
	}
	for _, tb := range u.Tables {
		out[tb.Name] = true
	}
	return out
}

// THE ONE LIST (js.GlobalNames): every JavaScript or TypeScript global the
// generated modules and declarations reference is written globalThis.X, and
// the list is exactly the set they reference. The scan reads the generated
// output of every corpus unit and of the unit that declares every listed name
// (js.GlobalNames, jsGlobalsShadowSchema), so a new emitter line that names a
// global plainly, or a global the list does not carry, fails here.
func TestJsGeneratedOutputReachesEveryGlobalThroughGlobalThis(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, g := range jsKnownGlobals {
		known[g] = true
	}
	referenced := map[string]bool{}
	units := 0
	scan := func(label string, u *ir.Unit) {
		files, err := New().Generate(u, "js", Options{})
		if err != nil {
			t.Logf("%s: js refuses the unit (%v); not scanned", label, err)
			return
		}
		units++
		own := declaredNames(u)
		names := slices.Sorted(func(yield func(string) bool) {
			for k := range files {
				if !yield(k) {
					return
				}
			}
		})
		for _, name := range names {
			if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".d.ts") {
				continue
			}
			for _, use := range scanJsGlobals(label+"/"+name, string(files[name]), known) {
				if use.qualified {
					referenced[use.name] = true
					continue
				}
				if own[use.name] {
					continue // the unit's own class of that name
				}
				t.Errorf("%s: the global %s is named plainly; write globalThis.%s", use.at, use.name, use.name)
			}
		}
	}
	for _, dir := range jsGlobalCorpora(t) {
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			continue // a directory holding no unit (a runtime, a fixture tree)
		}
		u, err := New().Load(paths)
		if err != nil {
			t.Logf("%s: %v; not scanned", dir, err)
			continue
		}
		scan(dir, u)
	}
	scan("shadow", loadJsGlobalsShadow(t))
	if units < 10 {
		t.Fatalf("the scan read %d units; the corpora are not where the test looks", units)
	}
	listed := map[string]bool{}
	for _, g := range js.GlobalNames {
		listed[g] = true
		if !known[g] {
			t.Errorf("js.GlobalNames lists %s, which is no JavaScript or TypeScript global this test knows", g)
		}
		if !referenced[g] {
			t.Errorf("js.GlobalNames lists %s, which no generated module or declaration references", g)
		}
	}
	var missing []string
	for g := range referenced {
		if !listed[g] {
			missing = append(missing, g)
		}
	}
	sort.Strings(missing)
	for _, g := range missing {
		t.Errorf("generated output references globalThis.%s, which js.GlobalNames does not list", g)
	}
}

// jsGlobalsShadowSchema declares a type for every listed global the checker
// accepts as a schema name, beside fields that drive every builtin the
// packet and table code calls: 64-bit integers (BigInt), byte arrays
// (Uint8Array), floats (Math.fround), an enum (Readonly in its declaration)
// and a table.
func jsGlobalsShadowSchema(names []string) string {
	var b strings.Builder
	b.WriteString("package clif\n\n")
	for _, g := range names {
		fmt.Fprintf(&b, "type %s { v uint8 }\n", g)
	}
	b.WriteString(`
enum Team { Red, Blue }

type Position
{
    ticks int64
    big uint64
    bytes_list [4]uint8
    f float32
    d float64
    side Team
    c float32 | min = 0, max = 1, resolution = 0.01
`)
	for i, g := range names {
		fmt.Fprintf(&b, "    s%d %s\n", i, g)
	}
	b.WriteString(`}

table Shelf
{
    a float32
    b float64
    n int64
    u uint64
    side Team
    label string(16)
}
`)
	return b.String()
}

// jsGlobalsShadowNames is js.GlobalNames less the names the checker already
// refuses as schema names (a reserved word in some target), each of which is
// checked to be refused.
func jsGlobalsShadowNames(t *testing.T) []string {
	t.Helper()
	var ok []string
	for _, g := range js.GlobalNames {
		dir := t.TempDir()
		src := "package clif\n\ntype " + g + " { v uint8 }\n"
		path := filepath.Join(dir, "Probe.schema")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New().Load([]string{path}); err != nil {
			continue
		}
		ok = append(ok, g)
	}
	return ok
}

// jsGlobalsConsumer is a strict TypeScript consumer of the declarations: an
// enum's declared object (globalThis.Readonly beside a schema class
// Readonly), a byte array read as the platform Uint8Array beside a schema
// class Uint8Array, and the flat writer's DataView beside a schema DataView.
const jsGlobalsConsumer = `import { Position, Team, WritePosition } from "./Probe.js";
import { WritePositionFlat } from "./ProbeFlat.js";

const p = new Position();
const n: number = p.bytesList.byteLength;
const side: Team = Team.Blue;
// @ts-expect-error the enum object is read-only
Team.Red = 2;
const wrote: number = WritePositionFlat(new globalThis.DataView(new globalThis.ArrayBuffer(64)), p);
export const used = [n, side, wrote, WritePosition];
`

// jsGlobalsRun loads every generated module under node, round-trips a
// Position through the packet writer and reader, writes it flat, and saves
// and loads a Shelf through the fixed table form.
const jsGlobalsRun = `import { pathToFileURL } from 'node:url';
import { Position, WritePosition, ReadPosition, Team } from './Probe.js';
import { WritePositionFlat } from './ProbeFlat.js';
import { Shelf, TableFixedReport } from './ClifTable.js';
import { ShelfFixedMeasure, ShelfFixedSave, ShelfFixedLoad, ShelfFixedNewPlan } from './ProbeTable.js';
const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);
IMPORTS

const value = new Position();
value.ticks = -5n;
value.big = 18446744073709551615n;
value.bytesList.set([1, 2, 3, 4]);
value.f = 1.5;
value.d = -2.25;
value.side = Team.Blue;
const ws = new WriteStream(new globalThis.Uint8Array(4096));
if (!WritePosition(ws, value)) { throw new globalThis.Error('the packet writer refused'); }
ws.flush();
const back = new Position();
if (!ReadPosition(new ReadStream(ws.data()), back)) { throw new globalThis.Error('the packet reader refused'); }
if (back.ticks !== -5n || back.big !== 18446744073709551615n || back.bytesList[3] !== 4 || back.f !== 1.5 || back.side !== Team.Blue) {
  throw new globalThis.Error('the packet round trip moved values');
}
if (WritePositionFlat(new globalThis.DataView(new globalThis.ArrayBuffer(4096)), value) <= 0) {
  throw new globalThis.Error('the flat writer refused');
}
const shelf = new Shelf();
shelf.a = 0.5; shelf.n = -7n; shelf.u = 9n; shelf.side = Team.Red;
const bytes = new globalThis.Uint8Array(ShelfFixedMeasure(1));
if (ShelfFixedSave([shelf], 1, bytes) < 0) { throw new globalThis.Error('the table save refused'); }
const loaded = [new Shelf()];
const report = new TableFixedReport();
if (ShelfFixedLoad(loaded, 1, bytes, bytes.length, ShelfFixedNewPlan(), report) !== 1) {
  throw new globalThis.Error('the table load refused: ' + globalThis.JSON.stringify(report));
}
if (loaded[0].n !== -7n || loaded[0].u !== 9n || loaded[0].a !== 0.5) { throw new globalThis.Error('the table round trip moved values'); }
console.log('OK');
`

// A unit declaring a type for every listed global the checker accepts —
// beside an int64 field (BigInt), a byte array (Uint8Array), floats, an enum
// (its declaration's Readonly) and a table — loads and runs under node, and
// its declarations pass tsc --strict for a consumer reading each of them.
func TestJsUnitDeclaringEveryGlobalRunsAndTypeChecks(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node, serialize.js and tsc (they run and type-check the generated modules)")
	files, err := New().Generate(loadJsGlobalsShadow(t), "js", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var imports []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".js") {
			imports = append(imports, "await import('./"+name+"');")
		}
	}
	sort.Strings(imports)
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`+"\n")
	write("consumer.ts", jsGlobalsConsumer)
	write("run.mjs", strings.Replace(jsGlobalsRun, "IMPORTS", strings.Join(imports, "\n"), 1))

	t.Run("node", func(t *testing.T) {
		t.Parallel()
		// a lane that sets SCHEMA_REQUIRE_NODE=1 says it has node and the
		// serialize.js runtime, so either missing fails there by name
		node := slowtest.Program(t, "node", "NODE", "SCHEMA_REQUIRE_NODE")
		runtime := os.Getenv("SERIALIZE_JS")
		if runtime == "" {
			runtime = "../../serialize.js"
		}
		index, err := filepath.Abs(filepath.Join(runtime, "src", "index.js"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(index); err != nil {
			slowtest.Require(t, "SCHEMA_REQUIRE_NODE", "no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout")
		}
		cmd := testproc.Command(t, node, "run.mjs")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SERIALIZE_JS_INDEX="+index)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "OK") {
			t.Fatalf("node: %v\n%s", err, out)
		}
	})
	t.Run("tsc", func(t *testing.T) {
		t.Parallel()
		tsc := slowtest.Program(t, "tsc", "TSC", "SCHEMA_REQUIRE_TSC")
		args := []string{"--strict", "--noEmit", "--target", "es2022", "--module", "nodenext", "--moduleResolution", "nodenext", "consumer.ts"}
		for name := range files {
			if strings.HasSuffix(name, ".d.ts") {
				args = append(args, name)
			}
		}
		cmd := testproc.Command(t, tsc, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc --strict: %v\n%s", err, out)
		}
	})
}

func loadJsGlobalsShadow(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(jsGlobalsShadowSchema(jsGlobalsShadowNames(t))), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit declaring every accepted global does not load: %v", err)
	}
	return u
}

// A declaration named after a TypeScript predefined type (unknown, never,
// symbol, number, bigint, undefined) cannot be declared as a type in a .d.ts,
// so the checker refuses it before a declaration file is written
// (internal/check, TestJavaScriptUnbindableNamesRefusedAsDeclarations). What
// is left type-checks: types named intrinsic and declare, which TypeScript
// reads as keywords only where it expects one, fields named like every
// refused declaration, and consts named after the predefined types, which
// are values and never types.
func TestJsDeclarationsBesideTypeScriptTypeNamesTypeCheck(t *testing.T) {
	t.Parallel()
	load := func(src string) (*ir.Unit, error) {
		dir := t.TempDir()
		path := filepath.Join(dir, "Wire.schema")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return New().Load([]string{path})
	}
	for _, name := range []string{"eval", "arguments", "undefined", "unknown", "never", "symbol", "number", "bigint"} {
		if _, err := load("package demo\n\ntype " + name + " { value uint32 }\n"); err == nil {
			t.Errorf("type %s loads: its JavaScript module or declaration file cannot bind that name", name)
		}
	}
	u, err := load(`package demo

const number = 1
const bigint uint64 = 18446744073709551615
const symbol = 3
const unknown = 4
const never = 0.5

type Clean { value uint32 }
type intrinsic { value uint32 }
type declare { value uint32 }

type Holder
{
    a Clean
    b intrinsic
    c declare
    eval uint32
    arguments uint32
    undefined uint32
    unknown uint32
    never uint32
    symbol uint32
    number uint32
    bigint uint32
}

table Entry
{
    part Clean
    number uint32
}
`)
	if err != nil {
		t.Fatalf("the control unit does not load: %v", err)
	}
	files, err := New().Generate(u, "js", Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("tsc", func(t *testing.T) {
		slowtest.Gate(t, "tsc (it type-checks the generated declarations)")
		tsc := slowtest.Program(t, "tsc", "TSC", "SCHEMA_REQUIRE_TSC")
		dir := t.TempDir()
		args := []string{"--strict", "--noEmit", "--target", "es2022", "--module", "nodenext", "--moduleResolution", "nodenext"}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".d.ts") {
				args = append(args, name)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := testproc.Command(t, tsc, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc --strict: %v\n%s", err, out)
		}
	})
}
