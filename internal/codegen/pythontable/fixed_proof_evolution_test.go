package pythontable_test

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestPythonFixedProofEvolution runs the shared lineage and file cases with
// the Python runner and the C++ fixed-form corpus.
func TestPythonFixedProofEvolution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form corpus")
	fixedproof.RunEvolutionRows(t, filepath.Join("..", "..", ".."), pythonFixedProofRunner{},
		"field-evolution", "retirement", "retired-runtime", "plan-execution", "interoperability", "file-envelope")
}

// TestPythonFixedProofEvolutionNumeric checks signed and float widening against
// independently generated C++ files and detects a changed signed widening plan.
func TestPythonFixedProofEvolutionNumeric(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	oldSchema := "package probe\nfixed table T {\n v int16\n f float32\n}\n"
	newSchema := "package probe\nfixed table T {\n v int32\n f float64\n added int32 = 77\n}\n"
	setup := "value[0].v = -7; value[0].f = 1.5f;"
	old := fixedproof.ParseCheckUnit(t, oldSchema)
	newer := fixedproof.ParseCheckUnit(t, newSchema)
	oldCase := fixedproof.RowCase{ID: "evolution-old", Schema: oldSchema, Root: "T", Body: 6, Count: 1, CppSetup: setup}
	newCase := fixedproof.RowCase{ID: "evolution-new", Schema: newSchema, Root: "T", Body: 16, Count: 1, CppSetup: setup}
	oldWire, verifyOld := fixedproof.CppOracle(t, oldCase, old)
	newWire, verifyNew := fixedproof.CppOracle(t, newCase, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer layout")
	}
	lineage := map[string][]pythontable.FixedLineageEntry{"T": {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: old.Tables["T"]}}}
	files, err := pythontable.GenerateLineage(newer, lineage)
	if err != nil {
		t.Fatal(err)
	}
	script := `from probe import T, LoadReport
with open('old','rb') as f:
    data = f.read()
r = LoadReport()
rows = T.fixed_load(data, report=r)
if len(rows) != 1 or rows[0].v != -7 or rows[0].f != 1.5 or rows[0].added != 77 or r.widened != 2:
    raise AssertionError('numeric_evolution_control')
with open('saved','wb') as f:
    f.write(T.fixed_save(rows))
print('numeric-evolution')
`
	out, saved := pythonFixedRun(t, files, script, map[string][]byte{"old": oldWire})
	if strings.TrimSpace(out) != "numeric-evolution" || !bytes.Equal(saved, newWire) {
		t.Fatal("widened values or saved bytes differ from C++")
	}
	if err := verifyOld(oldWire); err != nil {
		t.Fatalf("C++ old oracle: %v", err)
	}
	if err := verifyNew(saved); err != nil {
		t.Fatalf("C++ new oracle: %v", err)
	}
	oldFiles, err := pythontable.Generate(old)
	if err != nil {
		t.Fatal(err)
	}
	backward := `from probe import T, Refused
with open('new','rb') as f:
    data = f.read()
try:
    T.fixed_load(data)
except Refused:
    print('backward-refused')
else:
    raise AssertionError('backward-read accepted an unknown layout')
`
	if out, _ := pythonFixedRun(t, oldFiles, backward, map[string][]byte{"new": newWire}); strings.TrimSpace(out) != "backward-refused" {
		t.Fatalf("backward reader: %s", out)
	}
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("evolution strict analyzer: %v\n%s", err, out)
	}
	// The same oracle and probe must detect a moved live byte in the file.
	moved := bytes.Clone(oldWire)
	moved[len(moved)-6] ^= 1
	if verifyOld(moved) == nil {
		t.Fatal("file control: C++ accepted the changed value as the original")
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script), "old": moved})
		if err == nil || !strings.Contains(stderr, "numeric_evolution_control") {
			t.Fatalf("file control did not go red by name: %v\n%s", err, stderr)
		}
	}
	path := "probe/_schema/fixedform.py"
	source := string(files[path])
	writerAnchor := `records.append(root["prefix"] + image)`
	if strings.Count(source, writerAnchor) != 1 {
		t.Fatal("writer control anchor changed")
	}
	writerFiles := maps.Clone(files)
	writerFiles[path] = []byte(strings.Replace(source, writerAnchor, "image[0] ^= 1\n        "+writerAnchor, 1))
	_, damaged := pythonFixedRun(t, writerFiles, script, map[string][]byte{"old": oldWire})
	if bytes.Equal(damaged, newWire) || verifyNew(damaged) == nil {
		t.Fatal("writer control did not differ from the C++ oracle")
	}
	anchor := `_integer(source, src, size, op["signed"])`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("signed widening control anchor changed")
	}
	files[path] = []byte(strings.Replace(source, anchor, `_integer(source, src, size, False)`, 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script), "old": oldWire})
		if err == nil || !strings.Contains(stderr, "numeric_evolution_control") {
			t.Fatalf("signed widening control did not go red by name: %v\n%s", err, stderr)
		}
	}
}

// TestPythonFixedProofEvolutionHostile checks whole-batch refusal and proves
// that disabling the emitted boolean guard is detected.
func TestPythonFixedProofEvolutionHostile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	u, files, oracle, verify := pythonFixedMixed(t)
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("hostile strict analyzer: %v\n%s", err, out)
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(u.Tables["T"]))
	body := 20 + len(layout) + 8
	last := 20 + len(layout) + 8 + int(ir.TableFixedTypeBytes(u.Tables["T"]))
	inputs := map[string][]byte{"oracle": oracle}
	for _, tc := range []struct {
		name string
		at   int
		bit  byte
	}{
		{"invalid-bool", body, 2},
		{"last-hash", last, 1},
	} {
		data := bytes.Clone(oracle)
		data[tc.at] ^= tc.bit
		inputs[tc.name] = data
		if verify(data) == nil {
			t.Fatalf("file control %s did not differ from C++ oracle", tc.name)
		}
	}
	script := `from probe import T, Refused, LoadReport
with open('oracle','rb') as f:
    reference = f.read()
rows = T.fixed_load(reference)
report = LoadReport()
report.widened = 19
for name in ('invalid-bool', 'last-hash'):
    with open(name,'rb') as f:
        wire = f.read()
    try:
        T.fixed_load(wire, report=report)
    except Refused:
        pass
    else:
        raise AssertionError('hostile_control_' + name)
    if report.widened != 19 or T.fixed_save(rows) != reference:
        raise AssertionError('hostile_control_transaction')
print('hostile-refusal')
`
	if out, _ := pythonFixedRun(t, files, script, inputs); strings.TrimSpace(out) != "hostile-refusal" {
		t.Fatalf("hostile probe: %s", out)
	}
	path := "probe/_schema/fixedform.py"
	source := string(files[path])
	writerAnchor := `records.append(root["prefix"] + image)`
	if strings.Count(source, writerAnchor) != 1 {
		t.Fatal("hostile writer control anchor changed")
	}
	writerFiles := maps.Clone(files)
	writerFiles[path] = []byte(strings.Replace(source, writerAnchor, "image[0] ^= 1\n        "+writerAnchor, 1))
	writerScript := `from probe import T
with open('oracle','rb') as f:
    rows = T.fixed_load(f.read())
with open('saved','wb') as f:
    f.write(T.fixed_save(rows))
`
	_, changed := pythonFixedRun(t, writerFiles, writerScript, inputs)
	if bytes.Equal(changed, oracle) || verify(changed) == nil {
		t.Fatal("hostile writer control did not differ from the C++ oracle")
	}
	anchor := "if value > 1:\n        raise Refused(\"wire_damaged\", \"a bool or presence byte is not 0 or 1\")"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("boolean guard control anchor changed")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:\n        raise Refused(\"wire_damaged\", \"a bool or presence byte is not 0 or 1\")", 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		in := maps.Clone(inputs)
		in["main.py"] = []byte(pyrun.ImportPrelude + script)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
		if err == nil || !strings.Contains(stderr, "hostile_control_invalid-bool") {
			t.Fatalf("boolean guard control did not go red by name: %v\n%s", err, stderr)
		}
	}
}

// TestPythonFixedProofEvolutionHostileSweep poisons every record byte and
// checks every accepted value is in range and round-trips without repair.
func TestPythonFixedProofEvolutionHostileSweep(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	schema := "package probe\nfixed table T {\n flag bool\n n int8 | min = 0, max = 10\n s string(4)\n}\nfixed table B { p int16 }\n"
	tc := fixedproof.RowCase{ID: "hostile-sweep", Schema: schema, Root: "T", Body: 10, Count: 1,
		CppSetup: "value[0].flag = true; value[0].n = 5; value[0].s_length = 1; value[0].s[0] = 'a';"}
	u := fixedproof.ParseCheckUnit(t, schema)
	oracle, _ := fixedproof.CppOracle(t, tc, u)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := pythonFixedTree(t, files)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("hostile sweep strict analyzer: %v\n%s", err, out)
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(u.Tables["T"]))
	_, bHash, _ := fixedproof.RootFacts(u.Tables["B"])
	script := fmt.Sprintf(`from probe import T, B, Refused
with open('oracle','rb') as f:
    original = f.read()
forged = bytearray(original)
forged[8:16] = (%d).to_bytes(8, "little")
for data in (original, forged):
    try:
        B.fixed_load(data)
    except Refused:
        pass
    else:
        raise AssertionError("hostile_wrong_plan")
start = %d
count = 0
for at in range(start, len(original)):
    for byte in (0, 1, 2, 127, 128, 255):
        data = bytearray(original)
        data[at] = byte
        try:
            rows = T.fixed_load(data)
        except Refused:
            continue
        if len(rows) != 1 or type(rows[0].flag) is not bool or not 0 <= rows[0].n <= 10:
            raise AssertionError('hostile_sweep_control')
        if T.fixed_save(rows) != bytes(data):
            raise AssertionError('hostile_sweep_repaired')
        count += 1
print('hostile-sweep', count)
`, bHash, 20+len(layout))
	out, _ := pythonFixedRun(t, files, script, map[string][]byte{"oracle": oracle})
	var accepted int
	if _, err := fmt.Sscanf(out, "hostile-sweep %d", &accepted); err != nil || accepted == 0 {
		t.Fatalf("sweep did not accept any valid mutation: %q (%v)", out, err)
	}
	t.Logf("%d poisoned record-byte cases; %d accepted valid mutations", (len(oracle)-20-len(layout))*6, accepted)
	path := "probe/_schema/fixedform.py"
	source := string(files[path])
	anchor := `if value < spec.get("lo", 0) or value > spec.get("hi", len(spec.get("variants", []))):`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("range control anchor changed")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script), "oracle": oracle})
		if err == nil || !strings.Contains(stderr, "hostile_sweep_control") {
			t.Fatalf("range control did not go red by name: %v\n%s", err, stderr)
		}
	}
}

// TestPythonFixedProofEvolutionWideningCorpus checks the reference's old files
// against new readers using the actual old declarations as writer snapshots.
func TestPythonFixedProofEvolutionWideningCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form corpus")
	repo := filepath.Join("..", "..", "..")
	for _, tc := range []struct {
		row, check, setup string
		count             int
	}{
		{"int_widen", `assert [r.v for r in rows] == [-1, -32768, 32767] and report.widened == 3`, `for (int i = 0; i < 3; ++i) { value[i].lead = 0xAAAAAAAAu; value[i].trail = 0xBBBBBBBBu; } value[0].v = -1; value[1].v = -32768; value[2].v = 32767;`, 3},
		{"uint_widen", `assert rows[0].v == 65535 and report.widened == 1`, `value[0].lead = 0xAAAAAAAAu; value[0].v = 65535; value[0].trail = 0xBBBBBBBBu;`, 1},
		{"float_widen", `assert rows[0].v == -1234.5625 and report.widened == 1`, `value[0].lead = 0xAAAAAAAAu; value[0].v = -1234.5625; value[0].trail = 0xBBBBBBBBu;`, 1},
		{"array_elem_widen", `assert rows[0].vals == [-1, -32768, 32767, 0] and report.widened == 4`, `value[0].lead = 0xAAAAAAAAu; value[0].vals_count = 4; value[0].vals[0] = -1; value[0].vals[1] = -32768; value[0].vals[2] = 32767; value[0].vals[3] = 0; value[0].trail = 0xBBBBBBBBu;`, 1},
		{"range_widen", `assert rows[0].v == 100`, `value[0].lead = 0xAAAAAAAAu; value[0].v = 100; value[0].trail = 0xBBBBBBBBu;`, 1},
		{"cfloat_range_widen", `assert rows[0].aim == 0.5`, `value[0].lead = 1; value[0].aim = 0.5f; value[0].trail = 2;`, 1},
		{"union_arm_payload_widen", `assert rows[0].pick.type == 1 and rows[0].pick.alpha.x == 21 and rows[0].pick.alpha.y == 55 and rows[0].seq == 18`, `value[0].pick.type = PickType::Alpha; value[0].pick.alpha.x = 21; value[0].seq = 18;`, 1},
	} {
		t.Run(tc.row, func(t *testing.T) {
			t.Parallel()
			old := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+tc.row+".schema")
			newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_"+tc.row+".schema")
			root := fixedproof.RootOf(t, old)
			entry, ok := fixedproof.LineageEntryOf(old, root)
			if !ok {
				t.Fatal("missing writer layout")
			}
			files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{root: {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: old.Tables[root]}}})
			if err != nil {
				t.Fatal(err)
			}
			dir := pythonFixedTree(t, files)
			if out, err := pyrun.Analyze(t, dir, newer.Package); err != nil {
				t.Fatalf("widening strict analyzer: %v\n%s", err, out)
			}
			wire, err := os.ReadFile(filepath.Join(repo, "build", "fixedform-corpus", "old_"+tc.row+".bin"))
			if err != nil {
				t.Fatalf("C++ reference corpus: %v", err)
			}
			oldSource, err := os.ReadFile(filepath.Join(repo, "test", "tables", "VOLD_"+tc.row+".schema"))
			if err != nil {
				t.Fatal(err)
			}
			newSource, err := os.ReadFile(filepath.Join(repo, "test", "tables", "VNEW_"+tc.row+".schema"))
			if err != nil {
				t.Fatal(err)
			}
			oldOracle := fixedproof.ParseCheckUnit(t, strings.Replace(string(oldSource), "package "+old.Package, "package probe", 1))
			newOracle := fixedproof.ParseCheckUnit(t, strings.Replace(string(newSource), "package "+newer.Package, "package probe", 1))
			_, _, oldBody := fixedproof.RootFacts(oldOracle.Tables[root])
			_, verifyOld := fixedproof.CppOracle(t, fixedproof.RowCase{ID: tc.row + "-old", Root: root, Body: oldBody, Count: tc.count, CppSetup: tc.setup}, oldOracle)
			if err := verifyOld(wire); err != nil {
				t.Fatalf("old reference corpus differs from live oracle: %v", err)
			}
			_, _, newBody := fixedproof.RootFacts(newOracle.Tables[root])
			newBytes, verifyNew := fixedproof.CppOracle(t, fixedproof.RowCase{ID: tc.row + "-new", Root: root, Body: newBody, Count: tc.count, CppSetup: tc.setup}, newOracle)
			script := fmt.Sprintf(`from %s import %s as Root, LoadReport
with open('old', 'rb') as f:
    wire = f.read()
report = LoadReport()
rows = Root.fixed_load(wire, report=report)
%s
saved = Root.fixed_save(rows)
again = Root.fixed_load(saved)
assert Root.fixed_save(again) == saved
with open('saved', 'wb') as f:
    f.write(saved)
print('widening-corpus')
`, newer.Package, root, tc.check)
			out, saved := pythonFixedRun(t, files, script, map[string][]byte{"old": wire})
			if strings.TrimSpace(out) != "widening-corpus" || !bytes.Equal(saved, newBytes) {
				t.Fatalf("widening corpus: output %q or saved bytes differ from C++: python=%x cpp=%x", out, saved, newBytes)
			}
			if err := verifyNew(saved); err != nil {
				t.Fatalf("C++ refused widened Python save: %v", err)
			}
			// A changed old live value must fail the same cross-language byte
			// comparison, and a changed emitted writer must do so independently.
			mutant := bytes.Clone(wire)
			mutant[len(mutant)-1] ^= 1
			if verifyOld(mutant) == nil {
				t.Fatal("file control was accepted as the original by C++")
			}
			path := newer.Package + "/_schema/fixedform.py"
			source := string(files[path])
			anchor := `records.append(root["prefix"] + image)`
			if strings.Count(source, anchor) != 1 {
				t.Fatal("writer control anchor changed")
			}
			writerFiles := maps.Clone(files)
			writerFiles[path] = []byte(strings.Replace(source, anchor, "image[0] ^= 1\n        "+anchor, 1))
			control := fmt.Sprintf(`from %s import %s as Root
with open('old','rb') as f:
    rows = Root.fixed_load(f.read())
with open('expected','rb') as f:
    expected = f.read()
if Root.fixed_save(rows) != expected:
    raise AssertionError('widening_control')
`, newer.Package, root)
			for _, c := range []struct {
				name  string
				files map[string][]byte
				input []byte
			}{{"file", files, mutant}, {"writer", writerFiles, wire}} {
				for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
					dir := pythonFixedTree(t, c.files)
					_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + control), "old": c.input, "expected": newBytes})
					if err == nil || !strings.Contains(stderr, "widening_control") {
						t.Fatalf("%s control did not go red by name: %v\n%s", c.name, err, stderr)
					}
				}
			}
		})
	}
}

// TestPythonFixedProofEvolutionWriterBounds holds the historical writer's
// bounds on the C++ corpus and checks the guards with emitted-runtime faults.
func TestPythonFixedProofEvolutionWriterBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and C++ fixed-form corpus")
	repo := filepath.Join("..", "..", "..")
	for _, tc := range []struct {
		row, anchor string
		mutants     []string
	}{
		{"range_widen", `if value < spec.get("lo", 0) or value > spec.get("hi", len(spec.get("variants", []))):`, []string{"hostile_range_widen.bin", "hostile_range_widen_array.bin", "hostile_range_widen_u64.bin"}},
		{"cfloat_range_widen", `if spec.get("ranged", False) and not spec["lo"] <= value <= spec["hi"]:`, []string{"hostile_cfloat_range_widen.bin"}},
	} {
		t.Run(tc.row, func(t *testing.T) {
			t.Parallel()
			old := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+tc.row+".schema")
			newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_"+tc.row+".schema")
			root := fixedproof.RootOf(t, old)
			entry, ok := fixedproof.LineageEntryOf(old, root)
			if !ok {
				t.Fatal("missing writer layout")
			}
			files, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{root: {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: old.Tables[root]}}})
			if err != nil {
				t.Fatal(err)
			}
			inputs := map[string][]byte{}
			for _, name := range append([]string{"old_" + tc.row + ".bin"}, tc.mutants...) {
				inputs[name], err = os.ReadFile(filepath.Join(repo, "build", "fixedform-corpus", name))
				if err != nil {
					t.Fatalf("C++ reference corpus %s: %v", name, err)
				}
			}
			script := fmt.Sprintf(`from %s import %s as Root, Refused
with open(%q, 'rb') as f:
    good = f.read()
assert len(Root.fixed_load(good)) == 1
for name in %s:
    with open(name, 'rb') as f:
        wire = f.read()
    try:
        Root.fixed_load(wire)
    except Refused:
        pass
    else:
        raise AssertionError('writer_bounds_control_' + name)
print('writer-bounds')
`, newer.Package, root, "old_"+tc.row+".bin", pythonStringList(tc.mutants))
			if out, _ := pythonFixedRun(t, files, script, inputs); strings.TrimSpace(out) != "writer-bounds" {
				t.Fatalf("writer bounds: %s", out)
			}
			path := newer.Package + "/_schema/fixedform.py"
			source := string(files[path])
			if strings.Count(source, tc.anchor) != 1 {
				t.Fatalf("writer bounds control anchor count %d", strings.Count(source, tc.anchor))
			}
			files[path] = []byte(strings.Replace(source, tc.anchor, "if False:", 1))
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := pythonFixedTree(t, files)
				in := maps.Clone(inputs)
				in["main.py"] = []byte(pyrun.ImportPrelude + script)
				_, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
				if err == nil || !strings.Contains(stderr, "writer_bounds_control_") {
					t.Fatalf("writer bounds control did not go red by name: %v\n%s", err, stderr)
				}
			}
		})
	}
}

func pythonStringList(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Quote(value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// TestPythonFixedProofEvolutionRetirementControl verifies that the generated
// retirement floor refuses a known old writer and that removing it is detected.
func TestPythonFixedProofEvolutionRetirementControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	oldSchema := "package probe\nfixed table T {\n x int32\n}\n"
	newSchema := "package probe\nfixed table T {\n x int32\n y int32 = 55\n}\n"
	old := fixedproof.ParseCheckUnit(t, oldSchema)
	newer := fixedproof.ParseCheckUnit(t, newSchema)
	oldWire, verifyOld := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "retired-old", Root: "T", Body: 4, Count: 1, CppSetup: "value[0].x = 7;"}, old)
	newWire, verifyNew := fixedproof.CppOracle(t, fixedproof.RowCase{ID: "retired-new", Root: "T", Body: 8, Count: 1, CppSetup: "value[0].x = 7;"}, newer)
	entry, ok := fixedproof.LineageEntryOf(old, "T")
	if !ok {
		t.Fatal("missing writer layout")
	}
	e := pythontable.FixedLineageEntry{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Writer: old.Tables["T"], Retired: true}
	retired, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {e}})
	if err != nil {
		t.Fatal(err)
	}
	dir := pythonFixedTree(t, retired)
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("retirement strict analyzer: %v\n%s", err, out)
	}
	e.Retired = false
	live, err := pythontable.GenerateLineage(newer, map[string][]pythontable.FixedLineageEntry{"T": {e}})
	if err != nil {
		t.Fatal(err)
	}
	badFile := bytes.Clone(newWire)
	badFile[len(badFile)-8-8] ^= 1
	if verifyNew(badFile) == nil {
		t.Fatal("retirement file control was accepted by C++")
	}
	script := `from probe import T, Refused
with open('old','rb') as f: old = f.read()
with open('new','rb') as f: new = f.read()
with open('bad','rb') as f: bad = f.read()
for name, data in (('old', old), ('bad', bad)):
    try:
        T.fixed_load(data)
    except Refused:
        pass
    else:
        raise AssertionError('retirement_control_' + name)
rows = T.fixed_load(new)
assert len(rows) == 1 and rows[0].x == 7 and rows[0].y == 55
with open('saved','wb') as f: f.write(T.fixed_save(rows))
print('retirement-floor')
`
	inputs := map[string][]byte{"old": oldWire, "new": newWire, "bad": badFile}
	out, saved := pythonFixedRun(t, retired, script, inputs)
	if strings.TrimSpace(out) != "retirement-floor" || !bytes.Equal(saved, newWire) {
		t.Fatal("retirement probe differs from the C++ reference")
	}
	if err := verifyOld(oldWire); err != nil {
		t.Fatalf("old reference: %v", err)
	}
	writer := maps.Clone(retired)
	path := "probe/_schema/fixedform.py"
	anchor := `records.append(root["prefix"] + image)`
	source := string(writer[path])
	if strings.Count(source, anchor) != 1 {
		t.Fatal("retirement writer control anchor changed")
	}
	writer[path] = []byte(strings.Replace(source, anchor, "image[0] ^= 1\n        "+anchor, 1))
	_, changed := pythonFixedRun(t, writer, script, inputs)
	if bytes.Equal(changed, newWire) || verifyNew(changed) == nil {
		t.Fatal("retirement writer control did not change the C++ oracle bytes")
	}
	// Replace only the generated plan registration with the non-retired plan.
	// This plants the removed floor in a copy of the real generated artifact.
	withoutFloor := maps.Clone(retired)
	if bytes.Equal(withoutFloor["probe/_fixed.py"], live["probe/_fixed.py"]) {
		t.Fatal("retired and live plan controls are identical")
	}
	withoutFloor["probe/_fixed.py"] = live["probe/_fixed.py"]
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, withoutFloor)
		in := maps.Clone(inputs)
		in["main.py"] = []byte(pyrun.ImportPrelude + script)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
		if err == nil || !strings.Contains(stderr, "retirement_control_old") {
			t.Fatalf("retirement control did not go red by name: %v\n%s", err, stderr)
		}
	}
}

func init() {
	pythonFixedSetups["retirement"] = "rows[1].a = 10\nrows[1].b = 20\nrows[1].c = 30"
	pythonFixedSetups["retired-runtime"] = "rows[1].x = 44\nrows[1].y = 55\nrows[1].z = 66"
	pythonFixedSetups["field-evolution"] = "rows[1].b = 100\nrows[1].c = 200"
	pythonFixedSetups["plan-execution"] = "rows[1].a = 10\nrows[1].sub.p = 30\nrows[1].sub.q = 40\nrows[1].xs = [50, 60]"
	pythonFixedSetups["interoperability"] = "rows[1].a = 42\nrows[1].s = 'test'"
}

// TestPythonFixedProofEvolutionValues compares each selected row with a live
// C++ writer in both directions.
func TestPythonFixedProofEvolutionValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	fixedproof.RunValueRows(t, pythonFixedProofRunner{}, "retirement", "retired-runtime", "field-evolution", "plan-execution", "interoperability", "file-envelope")
}

// TestPythonFixedProofEvolutionNegativeControl plants file and writer faults
// independently for each selected row.
func TestPythonFixedProofEvolutionNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and live C++ oracle")
	fixedproof.RunNegativeControlRows(t, pythonFixedProofRunner{}, "retirement", "retired-runtime", "field-evolution", "plan-execution", "interoperability", "file-envelope")
}

// TestPythonFixedProofEvolutionStrict checks the generated packages for the
// shared evolution proof rows with the pinned strict analyzer.
func TestPythonFixedProofEvolutionStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python strict analyzer")
	selected := map[string]bool{"retirement": true, "retired-runtime": true, "field-evolution": true, "plan-execution": true, "interoperability": true, "file-envelope": true}
	for _, tc := range fixedproof.Cases() {
		if !selected[tc.ID] {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			files, err := pythontable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := pythonFixedTree(t, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("evolution strict analyzer: %v\n%s", err, out)
			}
		})
	}
}
