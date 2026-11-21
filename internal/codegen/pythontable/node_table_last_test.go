package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

const nodeLastSchema = `package mapdemo
fixed table Item { count int32 }
table Crews {
    members map[uint32][..2]*Item
    past int32 | was = "after"
%s
}
`

func TestPythonNodeTableLast(t *testing.T) {
	slowtest.Gate(t, "pinned Python node table order and Go wire oracle")
	nodeTableLastProbe(t, "")
}

func TestPythonNodeTableLastNegativeControl(t *testing.T) {
	slowtest.Gate(t, "pinned Python node table order fault control")
	nodeTableLastProbe(t, "decode")
}

func TestPythonNodeTableLastPrecedenceControl(t *testing.T) {
	slowtest.Gate(t, "pinned Python diagnostic precedence fault control")
	nodeTableLastProbe(t, "walk")
}

func nodeTableLastProbe(t *testing.T, plant string) {
	t.Helper()
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
	for _, known := range []bool{false, true} {
		extra := ""
		if known {
			extra = "value *Item"
		}
		u := loadUnit(t, fmt.Sprintf(nodeLastSchema, extra))
		model := tabletext.NewModel(u)
		for _, c := range []struct {
			name   string
			wire   []byte
			accept bool
		}{{"golden", good, true}, {"after_node", bad, false}, {"mismatch_before_node", mismatch, false}} {
			var report tabletext.Report
			ok, e := tablewire.Decode(model, model.New(model.Lookup("Crews")), c.wire, &report)
			if ok != c.accept || (!ok && tablewire.RefusalReason(e) != "wire_damaged") {
				t.Fatalf("Go oracle known=%v %s: ok=%v err=%v", known, c.name, ok, e)
			}
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if plant != "" {
			old := "        if root and ctx.saw_node:\n            raise Refused(\"wire_damaged\", \"the node table is not the root body's last field\")\n"
			if plant == "walk" {
				old = "        if saw_node:\n            raise Refused(\"wire_damaged\", \"the node table is not the root body's last field\")\n"
			}
			found := 0
			for name, data := range files {
				if strings.HasSuffix(name, "/_schema/load.py") && strings.Contains(string(data), old) {
					files[name] = []byte(strings.Replace(string(data), old, "", 1))
					found++
				}
			}
			if found != 1 {
				t.Fatalf("could not plant node ordering fault: %d", found)
			}
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs := map[string][]byte{"good.bin": good, "bad.bin": bad, "mismatch.bin": mismatch, "probe.py": []byte(pyrun.ImportPrelude + `from pathlib import Path
from mapdemo import Crews, Refused
from mapdemo._schema import LoadReport
assert Crews.load(Path("good.bin").read_bytes()).save() == Path("good.bin").read_bytes()
for name in ("bad.bin", "mismatch.bin"):
    report = LoadReport()
    report.unknown, report.widened, report.retained, report.retain_lost = 77, 66, 55, 44
    try:
        Crews.load(Path(name).read_bytes(), report=report)
    except Refused as err:
        assert err.reason == "wire_damaged", (name, err.reason)
        assert (report.unknown, report.widened, report.retained, report.retain_lost) == (77, 66, 55, 44)
    else:
        raise AssertionError((name, "accepted"))
print("node-last PASS")
`)}
		if plant == "walk" {
			inputs["probe.py"] = []byte(strings.Replace(string(inputs["probe.py"]), `("bad.bin", "mismatch.bin")`, `("mismatch.bin",)`, 1))
		}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, e := pyrun.Try(t, pin, dir, "probe.py", inputs)
			if plant != "" {
				witness := "bad.bin', 'accepted'"
				if plant == "walk" {
					witness = "mismatch.bin', 'kind_mismatch'"
				}
				if e == nil || !strings.Contains(stderr, witness) {
					t.Fatalf("fault not detected known=%v: %v\n%s%s", known, e, out, stderr)
				}
			} else if e != nil || !strings.Contains(out, "node-last PASS") {
				t.Fatalf("probe known=%v: %v\n%s%s", known, e, out, stderr)
			}
		}
	}
}
