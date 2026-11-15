package cstable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN ABSENT OPTIONAL'S PAYLOAD AND A COUNTED ARRAY'S DEAD SLOTS ARE ZERO
// (docs/SPEC-TABLES.md §7.2; docs/rulings.json fixed-dead-bytes-are-zero). The
// C# cook writer writes the committed keyed_config and root_full files byte for
// byte in both orders: keyed_config's absent gunners leave no payload, and
// root_full's weapons slots past the live count stay zero.
func TestCsCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet (it runs the generated C# cook writer)")

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := cookCorpusUnit(t, filepath.Join(root, "tables", "examples"))

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)

	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probe := filepath.Join(dir, "CookProbe.cs")
	if err := os.WriteFile(probe, []byte(cookZeroProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probe)
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		sources = append(sources, filepath.Join(root, "..", "serialize.cs", "src", name))
	}

	csc, refs, runtime := csDotnetToolchain(t)
	app := filepath.Join(dir, "probe.dll")
	args := append([]string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + app}, sources...)
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	config := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	wireDir := filepath.Join(root, "testdata", "conformance", "tables", "wire")
	cookDir := filepath.Join(root, "testdata", "conformance", "tables", "cook-write")
	cmd = exec.Command(csDotnet(), "exec", app,
		filepath.Join(wireDir, "keyed_config.bin"),
		filepath.Join(wireDir, "root_full.bin"),
		out)
	cmd.Env = csDotnetEnv(dir, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, output)
	}

	cases := []struct{ name, little, big string }{
		{"keyed_config", "keyed_config.cook", "keyed_config-be.cook"},
		{"root_full", "root_full.cook", "root_full-be.cook"},
	}
	cooked := map[string][]byte{}
	for _, c := range cases {
		for _, order := range []struct{ suffix, fixture string }{{"", c.little}, {"-be", c.big}} {
			got, err := os.ReadFile(filepath.Join(out, c.name+order.suffix))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(cookDir, order.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s%s differs from the committed cook at byte %d", c.name, order.suffix, cookDifference(got, want))
			}
			cooked[c.name+order.suffix] = got
		}
	}

	model := tabletext.NewModel(u)
	keyed := model.New(model.Lookup("KeyedConfig"))
	if !model.Read(keyed, mustRead(t, filepath.Join(root, "testdata", "conformance", "tables", "json", "keyed_config.json")), new(tabletext.Report)) {
		t.Fatal("keyed_config's text did not read")
	}
	keyedLayout := ir.RecordLayout(u, u.Tables["KeyedConfig"])
	hullsField := keyedLayout.FieldByName("hulls")
	hullsFacts := ir.BlockFieldOf(u, hullsField.Field, hullsField.Offset, false)
	hullLayout := ir.RecordLayout(u, u.Tables["HullConfig"])
	turretsField := hullLayout.FieldByName("turrets")
	turretsFacts := ir.BlockFieldOf(u, turretsField.Field, turretsField.Offset, false)
	turretLayout := ir.RecordLayout(u, u.Tables["TurretConfig"])
	gunnerField := turretLayout.FieldByName("gunner")
	gunnerFacts := ir.BlockFieldOf(u, gunnerField.Field, gunnerField.Offset, false)
	if !gunnerFacts.Optional {
		t.Fatal("TurretConfig.gunner is not an optional")
	}
	hulls := keyed.Fields[keyedFieldIndex(t, keyed, "hulls")]
	absent := 0
	for i, hullCell := range hulls.Elems {
		hullBase := int64(64) + hullsField.Offset + int64(i)*hullsFacts.ElemSize
		turrets := hullCell.Tab.Fields[keyedFieldIndex(t, hullCell.Tab, "turrets")]
		for j, turretCell := range turrets.Elems {
			gunner := turretCell.Tab.Fields[keyedFieldIndex(t, turretCell.Tab, "gunner")]
			if gunner.Present {
				continue
			}
			turretBase := hullBase + turretsField.Offset + int64(j)*turretsFacts.ElemSize
			from := turretBase + gunnerField.Offset
			to := turretBase + gunnerFacts.PresentOffset + 1
			for _, suffix := range []string{"", "-be"} {
				requireZero(t, "keyed_config"+suffix+" absent gunner", cooked["keyed_config"+suffix], from, to)
			}
			absent++
		}
	}
	if absent == 0 {
		t.Fatal("keyed_config holds no absent gunner")
	}

	rootValue := model.New(model.Lookup("RootConfig"))
	if !model.Read(rootValue, mustRead(t, filepath.Join(root, "testdata", "conformance", "tables", "json", "root_full.json")), new(tabletext.Report)) {
		t.Fatal("root_full's text did not read")
	}
	rootLayout := ir.RecordLayout(u, u.Tables["RootConfig"])
	weaponsField := rootLayout.FieldByName("weapons")
	weaponsFacts := ir.BlockFieldOf(u, weaponsField.Field, weaponsField.Offset, false)
	if !weaponsFacts.Counted {
		t.Fatal("RootConfig.weapons is not a counted array")
	}
	weapons := rootValue.Fields[keyedFieldIndex(t, rootValue, "weapons")]
	live := int64(weapons.Count)
	if live <= 0 || live >= weaponsFacts.ArrayBound {
		t.Fatalf("root_full's weapons holds %d live of %d slots, want a count below capacity", live, weaponsFacts.ArrayBound)
	}
	from := int64(64) + weaponsField.Offset + live*weaponsFacts.ElemSize
	to := int64(64) + weaponsField.Offset + weaponsFacts.ArrayBound*weaponsFacts.ElemSize
	for _, suffix := range []string{"", "-be"} {
		requireZero(t, "root_full"+suffix+" dead weapon slots", cooked["root_full"+suffix], from, to)
	}
}

func requireZero(t *testing.T, label string, got []byte, from, to int64) {
	t.Helper()
	if from < 0 || to > int64(len(got)) || from >= to {
		t.Fatalf("%s: span %d..%d is not inside the cooked image's %d bytes", label, from, to, len(got))
	}
	if !bytes.Equal(got[from:to], make([]byte, to-from)) {
		t.Fatalf("%s: bytes %d..%d are not zero: %x", label, from, to, got[from:to])
	}
}

func cookCorpusUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(path, data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", entry.Name(), perrs[0])
		}
		files = append(files, check.SourceFile{
			Path:  path,
			Name:  entry.Name(),
			Base:  strings.TrimSuffix(entry.Name(), ".schema"),
			Bytes: data,
			AST:   f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func keyedFieldIndex(t *testing.T, inst *tabletext.Instance, name string) int {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return i
		}
	}
	t.Fatalf("no field %q", name)
	return -1
}

func cookDifference(got, want []byte) int {
	for i := range min(len(got), len(want)) {
		if got[i] != want[i] {
			return i
		}
	}
	return min(len(got), len(want))
}

const cookZeroProbe = `using System;
using System.IO;
using Tabledemo;

static class CookProbe
{
    static void Main(string[] args)
    {
        string outDir = args[2];
        var keyedReport = new TableReport();
        var keyed = new KeyedConfig();
        if (Schema.KeyedConfigFixedLoad(keyed, File.ReadAllBytes(args[0]), new TableFixedEntry[1 << 14], keyedReport) != 1 || keyedReport.Refused || keyedReport.Damaged)
        {
            throw new Exception("keyed_config wire did not read: reason=" + keyedReport.Reason + " refused=" + keyedReport.Refused + " damaged=" + keyedReport.Damaged);
        }
        long keyedSize = Schema.KeyedConfigCookMeasure(keyed);
        if (keyedSize < 0)
        {
            throw new Exception("keyed_config cook measure");
        }
        foreach (bool big in new bool[] { false, true })
        {
            byte[] bytes = new byte[checked((int)keyedSize)];
            if (!Schema.KeyedConfigCook(keyed, bytes, big ? TableByteOrder.Big : TableByteOrder.Little))
            {
                throw new Exception("keyed_config cook");
            }
            File.WriteAllBytes(Path.Combine(outDir, "keyed_config" + (big ? "-be" : "")), bytes);
        }
        var rootReport = new TableReport();
        var root = new RootConfig();
        if (Schema.RootConfigFixedLoad(root, File.ReadAllBytes(args[1]), new TableFixedEntry[1 << 14], rootReport) != 1 || rootReport.Refused || rootReport.Damaged)
        {
            throw new Exception("root_full wire did not read: reason=" + rootReport.Reason + " refused=" + rootReport.Refused + " damaged=" + rootReport.Damaged);
        }
        long rootSize = Schema.RootConfigCookMeasure(root);
        if (rootSize < 0)
        {
            throw new Exception("root_full cook measure");
        }
        foreach (bool big in new bool[] { false, true })
        {
            byte[] bytes = new byte[checked((int)rootSize)];
            if (!Schema.RootConfigCook(root, bytes, big ? TableByteOrder.Big : TableByteOrder.Little))
            {
                throw new Exception("root_full cook");
            }
            File.WriteAllBytes(Path.Combine(outDir, "root_full" + (big ? "-be" : "")), bytes);
        }
    }
}
`
