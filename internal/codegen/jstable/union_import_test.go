package jstable

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnionClassesImportTheTablesTheyBuild holds the JavaScript union emitter
// (internal/codegen/js emitTagUnion) to importing every table arm class its
// storage constructor instantiates, from the unit home that defines that class
// (<Home>Table.js when the union is in the unit's fixed-form closure, and
// <Home>Variable.js otherwise), and round-trips EVERY UNION OF THE REGISTERED
// JAVASCRIPT CORPUS through every wire it rides in node.
//
// THE CORPUS IS READ, NOT LISTED: every `generate --lang js` line of make/*.mk
// and make/checks/*.mk is one unit (jsUnionCorpus), plus the units the
// jstable leg's own tests build (jsUnionTestUnits). A schema added to a JS
// generate line is in this gate the day it is added.
//
// THE CASES ARE DERIVED, NOT WRITTEN: for each union the IR says which wires
// carry it, and the probe builds a value for each of them —
//   - packet: a Decl union has Write<Name>/Read<Name>; every arm (None and each
//     payload-free arm included) is constructed with `new <Name>()`, written to
//     a faked stream (a tape: no runtime, no socket), read back into a fresh
//     `new <Name>()` and compared;
//   - fixed: a union in the closure of a fixed-form root is found inside a
//     fresh `new <Root>()` by walking its pre-allocated storage, each arm is
//     selected in turn (every enclosing union arm, count and present flag on
//     the way set to reach it), and the root is saved and loaded through
//     <Root>FixedSave/<Root>FixedLoad and compared;
//   - variable: a union a carried variable-form root reaches is set, arm by
//     payload arm, on a value built along the field path the IR names (a table
//     arm's value is the `new <Name>()` storage class's own arm instance when
//     the union is outside the fixed closure), saved and loaded through
//     <Root>Save/<Root>Load and compared.
//
// THE GATE CANNOT BE ESCAPED: every discovered union must report a passing
// round trip on every wire the IR says carries it, and a union no wire carries
// fails by name — so an addition is either covered or red.
func TestUnionClassesImportTheTablesTheyBuild(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	units := append(jsUnionCorpus(t), jsUnionTestUnits()...)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{\"type\":\"module\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(unionProbeHarness), 0o600); err != nil {
		t.Fatal(err)
	}

	// expected[key] is the set of wires the IR says carry the union
	expected := map[string]map[string]bool{}
	var main strings.Builder
	main.WriteString("import { report } from \"./harness.mjs\";\n")
	for i, cu := range units {
		sub := fmt.Sprintf("u%02d", i)
		u := loadUnitPaths(t, cu.paths)
		files, err := js.Generate(u)
		if err != nil {
			t.Fatalf("%s: js.Generate: %v", cu.name, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: jstable.Generate: %v", cu.name, err)
		}
		for k, v := range tables {
			files[k] = v
		}
		for name, data := range files {
			p := filepath.Join(dir, sub, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		probe, exp := unionUnitProbe(cu.name, u, files)
		if len(exp) == 0 {
			continue
		}
		for k, v := range exp {
			expected[k] = v
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "probe.mjs"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&main, "await import(\"./%s/probe.mjs\").catch((e) => report.fail(%q, \"import\", e));\n", sub, cu.name)
	}
	main.WriteString("console.log(JSON.stringify(report.result()));\n")
	if len(expected) == 0 {
		t.Fatal("no unions discovered in the registered JavaScript corpus")
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("union probe did not run: %v\n%s", err, out)
	}
	var res struct {
		Covered  map[string][]string `json:"covered"`
		Failures []string            `json:"failures"`
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("union probe output is not its report: %v\n%s", err, out)
	}
	for _, f := range res.Failures {
		t.Errorf("union round trip failed: %s", f)
	}
	keys := make([]string, 0, len(expected))
	for k := range expected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	wires := 0
	for _, k := range keys {
		got := map[string]bool{}
		for _, w := range res.Covered[k] {
			got[w] = true
		}
		if len(expected[k]) == 0 {
			t.Errorf("union %s: no JavaScript wire carries it, so nothing round-trips it — give it a case or say why not", k)
		}
		t.Logf("%s: %s", k, strings.Join(res.Covered[k], " "))
		for w := range expected[k] {
			if !got[w] {
				t.Errorf("union %s: no passing %s round trip", k, w)
			}
			wires++
		}
	}
	t.Logf("unions round-tripped: %d across %d units, %d union-wire cases", len(keys), len(units), wires)
}

type unionCorpusUnit struct {
	name  string
	paths []string
}

// jsGenerateLine is one registered JavaScript generation: the inputs after
// `--out <dir>`.
var jsGenerateLine = regexp.MustCompile(`generate --lang js --out \S+ (.+)$`)

// jsUnionDerived maps an input the make wiring derives under build/ to the
// checked-in source it is made from. A build/ input missing here fails the
// test rather than leaving its unions out.
var jsUnionDerived = map[string]string{
	// make/checks/packet-text.mk: cp examples-wide/WideText.schema
	"build/packet-wide/source/WideText.schema": "examples-wide/WideText.schema",
	// make/js.mk tables-js-runtime-home: tables/examples plus a one-field AaaRow table
	"build/runtime-home-js/src": "tables/examples",
}

// jsUnionCorpus reads every `generate --lang js` line of the make wiring
// (make/*.mk, make/checks/*.mk) and returns each distinct input set once.
func jsUnionCorpus(t *testing.T) []unionCorpusUnit {
	t.Helper()
	var mks []string
	for _, g := range []string{"make/*.mk", "make/checks/*.mk"} {
		m, err := filepath.Glob(repoPath(g))
		if err != nil {
			t.Fatal(err)
		}
		mks = append(mks, m...)
	}
	sort.Strings(mks)
	seen := map[string]bool{}
	var out []unionCorpusUnit
	for _, mk := range mks {
		f, err := os.Open(mk)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var line strings.Builder
		for sc.Scan() {
			s := sc.Text()
			if strings.HasSuffix(s, "\\") {
				line.WriteString(strings.TrimSuffix(s, "\\"))
				line.WriteString(" ")
				continue
			}
			line.WriteString(s)
			full := line.String()
			line.Reset()
			m := jsGenerateLine.FindStringSubmatch(full)
			if m == nil {
				continue
			}
			var inputs []string
			for _, tok := range strings.Fields(m[1]) {
				if strings.HasPrefix(tok, ">") || strings.HasPrefix(tok, "2>") || tok == "&&" || tok == ";" || tok == "||" || tok == "|" {
					break
				}
				tok = strings.TrimSuffix(tok, ";")
				if strings.Contains(tok, "$") {
					t.Fatalf("%s: a js generate input the test cannot resolve: %q in %q", mk, tok, full)
				}
				if strings.HasPrefix(tok, "build/") {
					src, ok := jsUnionDerived[tok]
					if !ok {
						t.Fatalf("%s: js generate input %s is derived under build/ and has no source in jsUnionDerived: name the checked-in schema it is made from", mk, tok)
					}
					tok = src
				}
				inputs = append(inputs, tok)
			}
			if len(inputs) == 0 {
				t.Fatalf("%s: a js generate line with no input: %q", mk, full)
			}
			key := strings.Join(inputs, " ")
			if seen[key] {
				continue
			}
			seen[key] = true
			paths := make([]string, len(inputs))
			for i, in := range inputs {
				paths[i] = repoPath(in)
			}
			out = append(out, unionCorpusUnit{name: key, paths: paths})
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if len(out) == 0 {
		t.Fatal("no `generate --lang js` line found in make/*.mk")
	}
	return out
}

// jsUnionTestUnits are the union-bearing units this leg's own go tests build
// rather than a make rule (mapsarms_test.go, the A/RT pairs).
func jsUnionTestUnits() []unionCorpusUnit {
	var out []unionCorpusUnit
	for _, p := range []string{"tables/lists", "tables/maps", "tables/arms", "test/tables/A1.schema", "test/tables/A2.schema", "test/tables/RT1.schema", "test/tables/RT2.schema"} {
		out = append(out, unionCorpusUnit{name: p, paths: []string{repoPath(p)}})
	}
	return out
}

// unionUnitProbe writes one unit's probe module and returns, per union key,
// the wires the IR says carry it.
func unionUnitProbe(name string, u *ir.Unit, files map[string][]byte) (string, map[string]map[string]bool) {
	type unionAt struct {
		un     *ir.Union
		base   string
		packet bool
	}
	var unions []unionAt
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if un, ok := d.(*ir.Union); ok {
				unions = append(unions, unionAt{un, f.Base, true})
			}
		}
		for _, un := range f.TableUnions {
			unions = append(unions, unionAt{un, f.Base, false})
		}
	}
	exp := map[string]map[string]bool{}
	if len(unions) == 0 {
		return "", exp
	}

	home := runtimeHome(u)
	fixedRootsAll := jsFixedUnitRoots(u)
	fixedUnions := unionFixedClosure(fixedRootsAll)
	varHome := home + "Variable.js"
	var varRoots []*ir.Struct
	if _, ok := files[varHome]; ok {
		vc := newVariableClass(u)
		for _, f := range u.Files {
			for _, st := range variableRoots(u, f.Tables) {
				if vc.carried(st.Name) {
					varRoots = append(varRoots, st)
				}
			}
		}
	}
	vb := &unionVarBuilder{g: &variableGen{unit: u, variable: ir.VariableTables(u)}, fixed: fixedUnions}

	var p strings.Builder
	p.WriteString("import { report, roundTripPacket, roundTripFixed, roundTripVariable, locateUnion, applySteps } from \"../harness.mjs\";\n")
	bases := map[string]bool{}
	for _, ua := range unions {
		bases[ua.base] = true
	}
	baseList := make([]string, 0, len(bases))
	for b := range bases {
		baseList = append(baseList, b)
	}
	sort.Strings(baseList)
	for _, b := range baseList {
		fmt.Fprintf(&p, "const __m_%s = await import(\"./%s.js\");\n", b, b)
	}
	if len(varRoots) > 0 {
		fmt.Fprintf(&p, "const __V = await import(\"./%s\");\n", varHome)
		var names []string
		for n := range u.Tables {
			names = append(names, n)
		}
		for n := range u.Structs {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(&p, "const { %s } = __V;\n", strings.Join(names, ", "))
	}
	// the union registry the fixed walk recognises an enclosing union by
	p.WriteString("const __unions = new Map([\n")
	for _, ua := range unions {
		fmt.Fprintf(&p, "  [__m_%s.%s, __m_%s.%sType],\n", ua.base, ua.un.Name, ua.base, ua.un.Name)
	}
	p.WriteString("]);\n")

	for _, ua := range unions {
		key := name + ":" + ua.un.Name
		want := map[string]bool{}
		exp[key] = want
		mod := "__m_" + ua.base
		cls := mod + "." + ua.un.Name
		tag := mod + "." + ua.un.Name + "Type"
		var arms []string
		var payloadArms []ir.UnionVariant
		arms = append(arms, "\"None\"")
		for _, v := range ua.un.Variants {
			arms = append(arms, fmt.Sprintf("%q", ir.GoExportName(v.Name)))
			if !v.Void() {
				payloadArms = append(payloadArms, v)
			}
		}
		armList := "[" + strings.Join(arms, ", ") + "]"

		// construction: the storage class's constructor builds every arm
		fmt.Fprintf(&p, "report.run(%q, \"construct\", () => {\n  const u = new %s();\n  if (u.Type !== %s.None) { throw new Error(\"fresh union is not None\"); }\n", key, cls, tag)
		for _, v := range payloadArms {
			fmt.Fprintf(&p, "  if (u.%s === undefined) { throw new Error(\"arm %s has no storage\"); }\n", ir.GoExportName(v.Name), v.Name)
		}
		p.WriteString("});\n")

		if ua.packet {
			want["packet"] = true
			fmt.Fprintf(&p, "report.run(%q, \"packet\", () => roundTripPacket(%s, %s, %s.Write%s, %s.Read%s, %s));\n",
				key, cls, tag, mod, ua.un.Name, mod, ua.un.Name, armList)
		}
		if fixedUnions[ua.un.Name] {
			want["fixed"] = true
			var roots []string
			for _, st := range fixedRootsAll {
				api := ""
				for _, f := range u.Files {
					for _, tb := range f.Tables {
						if tb == st {
							api = f.Base + "Table.js"
						}
					}
				}
				roots = append(roots, fmt.Sprintf("[%q, %q]", st.Name, api))
			}
			fmt.Fprintf(&p, "await report.runAsync(%q, \"fixed\", async () => {\n", key)
			fmt.Fprintf(&p, "  const home = await import(\"./%sTable.js\");\n", home)
			fmt.Fprintf(&p, "  for (const [root, api] of [%s]) {\n", strings.Join(roots, ", "))
			p.WriteString("    const r = await import(\"./\" + api);\n")
			fmt.Fprintf(&p, "    const steps = locateUnion(new home[root](), %s, __unions);\n", cls)
			p.WriteString("    if (steps === null) { continue; }\n")
			fmt.Fprintf(&p, "    for (const arm of %s) {\n", armList)
			fmt.Fprintf(&p, "      roundTripFixed(() => new home[root](), (v) => { applySteps(v, steps).Type = %s[arm]; }, r[root + \"FixedMeasure\"], r[root + \"FixedSave\"], r[root + \"FixedLoad\"], r[root + \"FixedNewPlan\"], () => new home.TableFixedReport());\n", tag)
			p.WriteString("    }\n    return;\n  }\n  throw new Error(\"no fixed root's storage holds it\");\n});\n")
		}
		var root *ir.Struct
		for _, st := range varRoots {
			if vb.reachesStruct(st, ua.un, map[string]bool{}) {
				root = st
				break
			}
		}
		if root != nil && len(payloadArms) > 0 {
			want["variable"] = true
			fmt.Fprintf(&p, "report.run(%q, \"variable\", () => {\n", key)
			for _, v := range payloadArms {
				val := vb.armValue(ua.un, v, cls, 0)
				build := vb.structValue(root, ua.un, val, map[string]bool{})
				fmt.Fprintf(&p, "  roundTripVariable(() => %s, __V.%sSave, __V.%sLoad, () => new __V.TableVariableReport());\n", build, root.Name, root.Name)
			}
			p.WriteString("});\n")
		}
	}
	return p.String(), exp
}

// unionFixedClosure is every union a fixed-form root's closure reaches: by a
// field, through an arm, through a nested union's arm.
func unionFixedClosure(roots []*ir.Struct) map[string]bool {
	out := map[string]bool{}
	seen := map[string]bool{}
	var visitStruct func(st *ir.Struct)
	var visitUnion func(un *ir.Union)
	visitField := func(f *ir.Field) {
		if f == nil || f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visitStruct(r)
		case *ir.Union:
			visitUnion(r)
		}
	}
	visitStruct = func(st *ir.Struct) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	visitUnion = func(un *ir.Union) {
		if out[un.Name] {
			return
		}
		out[un.Name] = true
		for _, v := range un.Variants {
			visitField(v.F)
		}
	}
	for _, st := range roots {
		visitStruct(st)
	}
	return out
}

// unionVarBuilder renders, from the IR, a variable-form root value whose one
// path reaches a target union set to a chosen arm. Every other field is the
// table class's own declared default (variableGen.defaultExpr).
type unionVarBuilder struct {
	g     *variableGen
	fixed map[string]bool
}

func (b *unionVarBuilder) reachesField(f *ir.Field, target *ir.Union, seen map[string]bool) bool {
	if f == nil {
		return false
	}
	if f.IsMap() {
		return b.reachesField(f.MapEntry.Fields[len(f.MapEntry.Fields)-1], target, seen)
	}
	if un := unionOf(f); un != nil {
		return b.reachesUnion(un, target, seen)
	}
	if st := tableOf(f); st != nil {
		return b.reachesStruct(st, target, seen)
	}
	return false
}

func (b *unionVarBuilder) reachesStruct(st *ir.Struct, target *ir.Union, seen map[string]bool) bool {
	if seen["s:"+st.Name] {
		return false
	}
	seen["s:"+st.Name] = true
	for _, f := range st.Fields {
		if b.reachesField(f, target, seen) {
			return true
		}
	}
	return false
}

func (b *unionVarBuilder) reachesUnion(un, target *ir.Union, seen map[string]bool) bool {
	if un == target {
		return true
	}
	if seen["u:"+un.Name] {
		return false
	}
	seen["u:"+un.Name] = true
	for _, v := range un.Variants {
		if v.Body() {
			if b.reachesStruct(v.Ref, target, seen) {
				return true
			}
			continue
		}
		if b.reachesField(v.F, target, seen) {
			return true
		}
	}
	return false
}

// structValue is a `new <St>()` whose first field reaching target holds val.
func (b *unionVarBuilder) structValue(st *ir.Struct, target *ir.Union, val string, seen map[string]bool) string {
	for _, f := range st.Fields {
		if b.reachesField(f, target, map[string]bool{"s:" + st.Name: true}) {
			return fmt.Sprintf("((t) => { t.%s = %s; return t; })(new __V.%s())", f.Name, b.fieldValue(f, target, val), st.Name)
		}
	}
	return "new __V." + st.Name + "()"
}

// fieldValue is a value of field f's whole shape (map, array, optional or
// plain) holding val at the one element on the path.
func (b *unionVarBuilder) fieldValue(f *ir.Field, target *ir.Union, val string) string {
	if f.IsMap() {
		key, value := f.MapEntry.Fields[0], f.MapEntry.Fields[len(f.MapEntry.Fields)-1]
		return fmt.Sprintf("new Map([[%s, %s]])", b.g.defaultExpr(key), b.fieldValue(value, target, val))
	}
	elem := b.elemValue(f, target, val)
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		return fmt.Sprintf("Array.from({ length: %d }, () => (%s))", f.ArrayBound, elem)
	case f.Array != ir.ArrayNone:
		n := f.ArrayMin
		if n < 1 {
			n = 1
		}
		return fmt.Sprintf("Array.from({ length: %d }, () => (%s))", n, elem)
	}
	return elem
}

func (b *unionVarBuilder) elemValue(f *ir.Field, target *ir.Union, val string) string {
	if un := unionOf(f); un != nil {
		if un == target {
			return val
		}
		for _, v := range un.Variants {
			if v.Body() && b.reachesStruct(v.Ref, target, map[string]bool{"u:" + un.Name: true}) {
				return fmt.Sprintf("{ arm: %s, value: %s }", jsStr(v.Name), b.structValue(v.Ref, target, val, nil))
			}
			if !v.Void() && !v.Body() && b.reachesField(v.F, target, map[string]bool{"u:" + un.Name: true}) {
				return fmt.Sprintf("{ arm: %s, value: %s }", jsStr(v.Name), b.fieldValue(v.F, target, val))
			}
		}
	}
	if st := tableOf(f); st != nil {
		return b.structValue(st, target, val, nil)
	}
	return b.g.defaultExpr(f)
}

// armValue is `{ arm, value }` for one payload arm of un: a by-value table
// arm takes the storage class's own instance (`new <Union>().<Arm>`) when the
// union is outside the fixed closure, so the value round-tripped is the one
// the class constructor built.
func (b *unionVarBuilder) armValue(un *ir.Union, v ir.UnionVariant, cls string, depth int) string {
	return fmt.Sprintf("{ arm: %s, value: %s }", jsStr(v.Name), b.armPayload(un, v, cls, depth))
}

func (b *unionVarBuilder) armPayload(un *ir.Union, v ir.UnionVariant, cls string, depth int) string {
	switch {
	case v.Body():
		if !b.fixed[un.Name] && cls != "" {
			return fmt.Sprintf("new %s().%s", cls, ir.GoExportName(v.Name))
		}
		return "new __V." + v.Ref.Name + "()"
	case v.F.Array == ir.ArrayNone && v.F.Type.Pointer && tableOf(v.F) != nil:
		return "new __V." + tableOf(v.F).Name + "()"
	case v.F.Array == ir.ArrayNone && v.F.Type.Pointer && v.F.Type.Kind == ir.TString:
		return "\"x\""
	case v.F.Array == ir.ArrayNone && v.F.Type.Pointer && v.F.Type.Kind == ir.TBytes:
		return "new Uint8Array([1])"
	case v.F.Array == ir.ArrayNone && unionOf(v.F) != nil && depth < 4:
		inner := unionOf(v.F)
		for _, iv := range inner.Variants {
			if !iv.Void() {
				return b.armValue(inner, iv, "", depth+1)
			}
		}
	}
	return b.g.defaultExpr(v.F)
}

// unionProbeHarness is the node side: the faked packet stream, the three
// round trips, the fixed storage walk and the report.
const unionProbeHarness = `import assert from "node:assert/strict";

const covered = new Map();
const failures = [];
export const report = {
  pass(key, wire) {
    if (!covered.has(key)) { covered.set(key, []); }
    covered.get(key).push(wire);
  },
  fail(key, wire, e) {
    failures.push(key + " [" + wire + "]: " + (e && e.stack ? e.stack.split("\n").slice(0, 3).join(" | ") : String(e)));
  },
  run(key, wire, fn) {
    try { fn(); this.pass(key, wire); } catch (e) { this.fail(key, wire, e); }
  },
  async runAsync(key, wire, fn) {
    try { await fn(); this.pass(key, wire); } catch (e) { this.fail(key, wire, e); }
  },
  result() { return { covered: Object.fromEntries(covered), failures }; },
};

// THE TAPE: a faked serialize.js stream. Writing records each value the codec
// hands it, reading hands them back in order; no runtime, no socket. What it
// pins is that Write and Read walk the union the same way and that Read lands
// what Write saw.
class Tape {
  constructor(tape) { this.reading = tape !== undefined; this.tape = tape || []; this.at = 0; this.error = null; }
  take(ref) {
    if (this.reading) {
      if (this.at >= this.tape.length) { return false; }
      ref.value = this.tape[this.at++];
    } else {
      this.tape.push(ref.value);
    }
    return true;
  }
  serializeAlign() { return true; }
  serializeBytes(buf) {
    if (this.reading) {
      if (this.at >= this.tape.length) { return false; }
      buf.set(this.tape[this.at++]);
    } else {
      this.tape.push(Uint8Array.from(buf));
    }
    return true;
  }
}

// every other serialize<Kind>(ref, ...) the codec calls rides the tape as the
// value it holds, so a width or a range the runtime grows is carried unseen
function tape(t) {
  return new Proxy(t, {
    get(target, prop, recv) {
      if (prop in target) { return Reflect.get(target, prop, recv); }
      if (typeof prop === "string" && prop.startsWith("serialize")) { return (ref) => target.take(ref); }
      return undefined;
    },
  });
}

export function roundTripPacket(Cls, Tag, write, read, arms) {
  assert.equal(typeof write, "function", "no Write function");
  assert.equal(typeof read, "function", "no Read function");
  for (const arm of arms) {
    const src = new Cls();
    src.Type = Tag[arm];
    const w = new Tape();
    assert.ok(write(tape(w), src), "write " + arm);
    const dst = new Cls();
    const r = new Tape(w.tape);
    assert.ok(read(tape(r), dst), "read " + arm);
    assert.equal(r.at, w.tape.length, "read consumed what write wrote: " + arm);
    assert.deepStrictEqual(dst, src, "arm " + arm);
  }
}

export function roundTripFixed(make, mut, measure, save, load, newPlan, makeReport) {
  const src = make();
  mut(src);
  const buf = new Uint8Array(measure(1));
  const written = save([src], 1, buf);
  assert.strictEqual(written, buf.length);
  const dst = [make()];
  const plan = newPlan(64, 64);
  const rrep = makeReport();
  const count = load(dst, 1, buf, written, plan, rrep);
  assert.strictEqual(count, 1);
  assert.strictEqual(rrep.malformed, false);
  assert.strictEqual(rrep.refused, 0);
  assert.deepStrictEqual(dst[0], src);
}

export function roundTripVariable(make, save, load, makeReport) {
  const src = make();
  const bytes = save(src);
  assert.ok(bytes instanceof Uint8Array && bytes.length > 0, "save wrote nothing");
  const rrep = makeReport();
  const dst = load(bytes, rrep);
  assert.strictEqual(rrep.malformed, false);
  assert.strictEqual(rrep.refused, 0);
  assert.deepStrictEqual(dst, src);
}

// locateUnion walks a fresh fixed root's pre-allocated storage to the first
// instance of Target and returns the steps that make it reachable on the wire
// (an enclosing union's tag, an array count, an optional's present flag), each
// as [path from the root to the holder, setter]; the last step's holder is the
// union itself. null when the root does not hold it.
export function locateUnion(obj, Target, unions, path = []) {
  if (obj instanceof Target) { return [[path, () => {}]]; }
  const ownerTag = unions.get(obj.constructor);
  for (const key of Object.keys(obj)) {
    const val = obj[key];
    if (val === null || typeof val !== "object" || ArrayBuffer.isView(val) || val instanceof Map) { continue; }
    const pre = [];
    if (ownerTag !== undefined) {
      if (ownerTag[key] === undefined) { continue; }
      pre.push([path, (h) => { h.Type = ownerTag[key]; }]);
    }
    if ((key + "Present") in obj) { pre.push([path, (h) => { h[key + "Present"] = true; }]); }
    if (Array.isArray(val)) {
      if (val.length === 0 || val[0] === null || typeof val[0] !== "object") { continue; }
      if ((key + "Count") in obj) { pre.push([path, (h) => { h[key + "Count"] = Math.max(h[key + "Count"], 1); }]); }
      const sub = locateUnion(val[0], Target, unions, [...path, key, 0]);
      if (sub !== null) { return [...pre, ...sub]; }
      continue;
    }
    const sub = locateUnion(val, Target, unions, [...path, key]);
    if (sub !== null) { return [...pre, ...sub]; }
  }
  return null;
}

// applySteps runs locateUnion's steps on a fresh root and returns the union.
export function applySteps(root, steps) {
  let holder = root;
  for (const [path, set] of steps) {
    holder = root;
    for (const k of path) { holder = holder[k]; }
    set(holder);
  }
  return holder;
}
`
