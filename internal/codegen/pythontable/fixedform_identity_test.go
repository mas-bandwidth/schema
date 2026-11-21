package pythontable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/lockfile"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func pythonFixedIdentityFixture(t *testing.T, fixture string) (*compiler.Compiler, *ir.Unit, []string, string) {
	t.Helper()
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(repo, "internal", "lockfile", "testdata", fixture)
	dir := t.TempDir()
	for _, name := range []string{"Writer.schema", lockfile.FileName} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{filepath.Join(dir, "Writer.schema")}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return c, u, paths, base
}

func pythonFixedIdentityLock(t *testing.T, paths []string) *lockfile.Unit {
	t.Helper()
	lk, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("open recorded lock: %v %v", ok, err)
	}
	return lk
}

func TestPythonFixedIdentityRecordedV1(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins and the live C++ fixed-form writer")
	c, u, paths, base := pythonFixedIdentityFixture(t, "writer-v1/wide")
	old := pythonFixedIdentityLock(t, paths).Table("Row").Lineage[0]
	if old.Wire != 0xcd3d85d3dddbe9d3 {
		t.Fatal("genuine legacy identity moved")
	}
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	initial := pythonFixedIdentityLock(t, paths).Table("Row").Lineage
	if len(initial) != 2 || initial[0].Wire != old.Wire || initial[1].Wire == old.Wire {
		t.Fatal("prospective identity rewrote history")
	}
	source, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	wider := strings.Replace(string(source), "1208925819614629174706176", "2417851639229258349412352", 1)
	if err := os.WriteFile(paths[0], []byte(wider), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err = c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	entries := pythonFixedIdentityLock(t, paths).Table("Row").Lineage
	if len(entries) != 3 || entries[0].Wire != old.Wire || entries[2].Wire == entries[1].Wire || !bytes.Equal(entries[0].Digest, old.Digest) {
		t.Fatal("full-width widening lost recorded identities")
	}
	files, err := c.Generate(u, "python", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range []string{"good.bin", "forged-small.bin", "lawful-colliding-large.bin"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	if !bytes.Equal(inputs["forged-small.bin"], inputs["lawful-colliding-large.bin"]) {
		t.Fatal("legacy collision witness moved")
	}
	tc := fixedproof.RowCase{ID: "python-recorded-v1-current", Schema: wider, Root: "Row", Body: 16, Count: 1, CppSetup: `value[0].a=((serialize::int128_t)1<<80)+1;`}
	// The shared C++ harness names its namespace probe. Package names are
	// absent from fixed wire identity; verify that before using its oracle.
	tc.Schema = strings.Replace(wider, "package lockdemo", "package probe", 1)
	cppUnit := fixedproof.ParseCheckUnit(t, tc.Schema)
	_, cppHash, _ := fixedproof.RootFacts(cppUnit.Tables["Row"])
	if cppHash != entries[2].Wire {
		t.Fatal("package change altered fixed wire identity")
	}
	prospective, verify := fixedproof.CppOracle(t, tc, cppUnit)
	inputs["prospective"] = prospective
	probe := pyrun.ImportPrelude + `from lockdemo import Row, Refused, FixedLoadReport
with open('good.bin','rb') as f:good=f.read()
report=FixedLoadReport()
assert Row.load(good,report).a==1<<80 and report.layout_hash==0xcd3d85d3dddbe9d3
for name in ('forged-small.bin','lawful-colliding-large.bin'):
 with open(name,'rb') as f:bad=f.read()
 report.layout_hash=123;report.widened=456
 try:Row.load(bad,report)
 except Refused as err:assert err.reason=='out_of_range'
 else:raise AssertionError('fixed_recorded_writer_control')
 assert report.layout_hash==123 and report.widened==456
value=Row();value.a=(1<<80)+1
wire=value.save()
with open('prospective','rb') as f:expected=f.read()
assert wire==expected,'fixed_v2_wire_control'
assert int.from_bytes(wire[8:16],'little')!=0xcd3d85d3dddbe9d3
assert Row.load(wire).a==(1<<80)+1
with open('saved','wb') as f:f.write(wire)
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		inputs["main.py"] = []byte(probe)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil {
			t.Fatalf("recorded-v1 pipeline: %v\n%s", err, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if err := verify(saved); err != nil {
			t.Fatal(err)
		}
		if output, err := pyrun.Analyze(t, dir, "lockdemo"); err != nil {
			t.Fatalf("recorded identity strict mypy: %v\n%s", err, output)
		}
	}
	for _, control := range []string{"writer", "wire"} {
		broken := maps.Clone(files)
		name := "lockdemo/_fixed.py"
		marker := "fixed_recorded_writer_control"
		if control == "writer" {
			anchor := `"hi":1208925819614629174706176,`
			if !strings.Contains(string(broken[name]), anchor) {
				t.Fatal("historical check control lost its anchor")
			}
			broken[name] = []byte(strings.ReplaceAll(string(broken[name]), anchor, `"hi":2417851639229258349412352,`))
		} else {
			name = "lockdemo/_schema/fixedform.py"
			anchor := `root["prefix"] = root["hash"].to_bytes(8, "little")`
			if strings.Count(string(broken[name]), anchor) != 1 {
				t.Fatal("wire identity control lost its anchor")
			}
			broken[name] = []byte(strings.Replace(string(broken[name]), anchor, `root["prefix"] = (0xcd3d85d3dddbe9d3).to_bytes(8, "little")`, 1))
			marker = "fixed_v2_wire_control"
		}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := pythonFixedTree(t, broken)
			inputs["main.py"] = []byte(probe)
			_, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
			if err == nil || !strings.Contains(stderr, marker) {
				t.Fatalf("%s identity sabotage did not fail by name: %v\n%s", control, err, stderr)
			}
		}
	}
}

func TestPythonFixedIdentityTrustedLayoutOnly(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins")
	c, u, paths, _ := pythonFixedIdentityFixture(t, "writer-v6")
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	legacy := pythonFixedIdentityLock(t, paths).Table("Row").Lineage[0]
	if legacy.Wire != 0x61195401a4aa47fd || legacy.Writer != nil || len(legacy.Digest) != 0 || legacy.LegacyLayoutOnly {
		t.Fatal("v6 unknown provenance was inferred")
	}
	if _, err := c.Generate(u, "python", compiler.Options{}); err == nil || !strings.Contains(err.Error(), "checked writer declaration snapshot") {
		t.Fatalf("unknown historical writer did not hold generation: %v", err)
	}
	target := fmt.Sprintf("Row@0x%016x", legacy.Wire)
	if _, _, err := compiler.ImportSchemaLockWriter(u, paths, target, u); err == nil {
		t.Fatal("strict import erased writer facts")
	}
	if _, _, err := compiler.ImportSchemaLockWriterWithOptions(u, paths, target, u, compiler.SchemaLockWriterImportOptions{LegacyLayoutOnly: true}); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte(strings.Replace(string(source), "max = 10", "max = 20", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err = c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
		t.Fatal(err)
	}
	imported := pythonFixedIdentityLock(t, paths).Table("Row").Lineage[0]
	if !imported.LegacyLayoutOnly || imported.Writer == nil || imported.Wire != legacy.Wire || len(imported.Digest) != 0 {
		t.Fatal("trusted provenance or old key was lost")
	}
	files, err := c.Generate(u, "python", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	good := fixedproof.MakeFixedFile(legacy.Wire, legacy.Layout, binary.LittleEndian.AppendUint32(nil, 10))
	bad := fixedproof.MakeFixedFile(legacy.Wire, legacy.Layout, binary.LittleEndian.AppendUint32(nil, 11))
	probe := pyrun.ImportPrelude + `from lockdemo import Row, Refused
with open('good','rb') as f:good=f.read()
assert Row.load(good).a.n==10
with open('bad','rb') as f:bad=f.read()
try:Row.load(bad)
except Refused as err:assert err.reason=='out_of_range'
else:raise AssertionError('trusted historical bound lost')
v=Row();v.a.n=11
assert Row.load(v.save()).a.n==11
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "good": good, "bad": bad})
		if err != nil {
			t.Fatalf("trusted raw legacy identity: %v\n%s", err, stderr)
		}
	}
}

func TestPythonFixedIdentityRejectsUnrecordedSchemes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T { x int32 | min = 0, max = 10 }\n")
	e, _ := fixedproof.LineageEntryOf(u, "T")
	for _, control := range []string{"future", "erased", "layout-only-v2"} {
		entry := pythontable.FixedLineageEntry{Wire: e.Wire, Layout: e.Layout, Record: e.Record, Writer: e.Writer, Digest: e.Digest}
		switch control {
		case "future":
			entry.Digest = append([]byte(nil), entry.Digest...)
			entry.Digest[len("\x00schema-fixed-definitions")] = 3
		case "erased":
			entry.Digest = nil
		case "layout-only-v2":
			entry.LegacyLayoutOnly = true
		}
		if control != "layout-only-v2" {
			entry.Wire = ir.TableFixedLayoutHashRecorded(entry.Layout, entry.Digest)
		}
		wire := fixedproof.MakeFixedFile(entry.Wire, entry.Layout, binary.LittleEndian.AppendUint32(nil, 5))
		files, err := pythontable.GenerateLineage(u, map[string][]pythontable.FixedLineageEntry{"T": {entry}})
		if err != nil {
			t.Fatal(err)
		}
		probe := pyrun.ImportPrelude + `from probe import T, Refused
with open('wire','rb') as f:wire=f.read()
try:T.load(wire)
except Refused as err:assert err.reason=='layout_malformed'
else:raise AssertionError('unrecorded writer identity accepted')
`
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := pythonFixedTree(t, files)
			_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
			if err != nil {
				t.Fatalf("%s recorded scheme: %v\n%s", control, err, stderr)
			}
		}
	}
}
