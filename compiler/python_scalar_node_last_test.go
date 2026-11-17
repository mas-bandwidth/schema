package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonScalarNodeTableLastThroughDriver(t *testing.T) {
	slowtest.Gate(t, "public scalar fast reader and node table order")
	scalarNodeLastProbe(t, "")
}

func TestPythonScalarNodeTableLastFastGuardControl(t *testing.T) {
	slowtest.Gate(t, "public scalar fast reader node order fault control")
	scalarNodeLastProbe(t, "guard")
}

func TestPythonScalarNodeTableLastPrecedenceControl(t *testing.T) {
	slowtest.Gate(t, "public scalar fast reader precedence fault control")
	scalarNodeLastProbe(t, "walk")
}

func scalarNodeLastProbe(t *testing.T, plant string) {
	t.Helper()
	u := unitFromSource(t, `package scalarprobe
table Crews {
    past int32 | was = "after"
}
`)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	if plant != "" {
		old := "        if ctx.saw_node:\n            raise Refused(\"wire_damaged\", \"the node table is not the root body's last field\")\n"
		if plant == "walk" {
			old = "                _s_check_file(view, _id_table(view))\n"
		}
		found := 0
		for name, data := range files {
			if strings.HasSuffix(name, "/Probe.py") && strings.Contains(string(data), old) {
				replacement := ""
				if plant == "walk" {
					replacement = "                pass\n"
				}
				files[name] = []byte(strings.Replace(string(data), old, replacement, 1))
				found++
			}
		}
		if found != 1 {
			t.Fatalf("could not plant scalar %s fault: %d", plant, found)
		}
	}
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/map_crews.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(good) != 137 || good[72] != 0 || good[36] != 4 {
		t.Fatal("map_crews fixture layout changed")
	}
	bad := append([]byte(nil), good[:72]...)
	bad = append(bad, 3, 17, 3)
	bad = append(bad, good[72:]...)
	mismatch := append([]byte(nil), bad...)
	mismatch[36] = 8
	model := tabletext.NewModel(u)
	for _, c := range []struct {
		name   string
		wire   []byte
		accept bool
	}{{"good", good, true}, {"bad", bad, false}, {"mismatch", mismatch, false}} {
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(model.Lookup("Crews")), c.wire, &report)
		if ok != c.accept || (!ok && tablewire.RefusalReason(err) != "wire_damaged") {
			t.Fatalf("Go oracle %s: ok=%v err=%v", c.name, ok, err)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if plant == "" {
		if out, err := pyrun.Analyze(t, dir, "scalarprobe"); err != nil {
			t.Fatalf("strict mypy: %v\n%s", err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from pathlib import Path
from scalarprobe import Crews, Refused
Crews.load(Path("good.bin").read_bytes())
for name in ("bad.bin", "mismatch.bin"):
    try:
        Crews.load(Path(name).read_bytes())
    except Refused as err:
        assert err.reason == "wire_damaged", (name, err.reason)
    else:
        raise AssertionError((name, "accepted"))
`
	inputs := map[string][]byte{"good.bin": good, "bad.bin": bad, "mismatch.bin": mismatch, "probe.py": []byte(probe)}
	if plant == "walk" {
		inputs["probe.py"] = []byte(strings.Replace(probe, `("bad.bin", "mismatch.bin")`, `("mismatch.bin",)`, 1))
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "probe.py", inputs)
		if plant != "" {
			witness := "bad.bin', 'accepted'"
			if plant == "walk" {
				witness = "mismatch.bin', 'kind_mismatch'"
			}
			if err == nil || !strings.Contains(stderr, witness) {
				t.Fatalf("scalar %s control: %v\n%s%s", plant, err, out, stderr)
			}
		} else if err != nil {
			t.Fatalf("scalar probe: %v\n%s%s", err, out, stderr)
		}
	}
}

func TestPythonScalarNodeLastHelperNames(t *testing.T) {
	slowtest.Gate(t, "public scalar helper name collision")
	u := unitFromSource(t, `package collision
 table Foo { a int32 }
 table checked_Foo { b int32 }
 `)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	emitted := string(files["collision/Probe.py"])
	for _, name := range []string{"_check_file as _s_check_file", "def _s_checked_load_Foo(", "def _s_load_checked_Foo("} {
		if !strings.Contains(emitted, name) {
			t.Errorf("generated module lacks disjoint helper %q", name)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
	probe := pyrun.ImportPrelude + `from collision import Foo, checked_Foo
a=Foo(); a.a=11
b=checked_Foo(); b.b=22
assert Foo.load(a.save()).a == 11
assert checked_Foo.load(b.save()).b == 22
`
	inputs := map[string][]byte{"probe.py": []byte(probe)}
	pyrun.Run(t, dir, "probe.py", inputs)
	pyrun.RunFloor(t, dir, "probe.py", inputs)
}
