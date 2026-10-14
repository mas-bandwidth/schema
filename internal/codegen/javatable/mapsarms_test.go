package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type javaVariableFixture struct {
	name, root, wire, json string
}

// Every selected manifest instance exercises wire and JSON in both directions
// against the committed reference answers, including non-default union arms
// and map values (SPEC-TABLES §2.3, §2.4, §2.6, §2.8, §15, §16).
func TestMapsAndUnionArmsOnTheTableWireJava(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"mapdemo": true, "messagedemo": true, "scalars": true, "widedemo": true}
	units := map[string]string{}
	fixtures := map[string][]javaVariableFixture{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" && selected[fields[1]] {
			units[fields[1]] = filepath.Join(repo, fields[2])
		}
		if len(fields) == 5 && fields[0] == "instance" && selected[fields[2]] {
			fixtures[fields[2]] = append(fixtures[fields[2]], javaVariableFixture{fields[1], fields[3], filepath.Join(repo, fields[4]), filepath.Join(repo, "testdata", "conformance", "tables", "json", fields[1]+".json")})
		}
	}
	for _, name := range []string{"mapdemo", "messagedemo", "scalars", "widedemo"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if units[name] == "" || len(fixtures[name]) == 0 {
				t.Fatalf("manifest has no %s unit/instances", name)
			}
			javaVariableFixtures(t, units[name], fixtures[name])
		})
	}
	t.Run("armdemo", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(repo, "tables", "arms")
		u := unitFromDir(t, dir)
		model := tabletext.NewModel(u)
		var rows []javaVariableFixture
		for _, item := range []struct{ name, root string }{{"holder", "Holder"}, {"hand", "Hand"}, {"chain", "Chain"}, {"gate", "Gate"}, {"gate_text", "Gate"}, {"ring", "Ring"}, {"rack", "Rack"}, {"tray", "Tray"}, {"nest", "Nest"}} {
			wire := filepath.Join(repo, "testdata", "wire", "tables", "arms_"+item.name+".bin")
			data, err := os.ReadFile(wire)
			if err != nil {
				t.Fatal(err)
			}
			value := model.New(u.Tables[item.root])
			report := new(tabletext.Report)
			if ok, err := tablewire.Decode(model, value, data, report); !ok || err != nil || report.Malformed || report.Refused {
				t.Fatalf("Go reference %s: %v %+v", item.name, err, report)
			}
			text, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			json := filepath.Join(t.TempDir(), item.name+".json")
			if err := os.WriteFile(json, text, 0o644); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, javaVariableFixture{item.name, item.root, wire, json})
		}
		javaVariableFixtures(t, dir, rows)
	})
}

// Compile each unit once, then apply its native typed codecs to every row.
func javaVariableFixtures(t *testing.T, schema string, fixtures []javaVariableFixture) {
	t.Helper()
	u := unitFromDir(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixtures {
		if files[row.root+"Variable.java"] == nil {
			t.Fatalf("%s: %sVariable.java is not emitted", row.name, row.root)
		}
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	seen := map[string]bool{}
	for _, row := range fixtures {
		if seen[row.root] {
			continue
		}
		seen[row.root] = true
		name := "MapsDriver_" + row.root
		path := filepath.Join(dir, name+".java")
		source := strings.ReplaceAll(variableRoundTripDriver(pkg, row.root), "Driver", name)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, row := range fixtures {
		out, err := exec.Command(java, "-ea", "-cp", classes, "MapsDriver_"+row.root, row.wire, row.json).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ok") {
			t.Errorf("%s: %v\n%s", row.name, err, out)
		} else {
			t.Log(fmt.Sprintf("%s: wire and JSON match the reference", row.name))
		}
	}
}
