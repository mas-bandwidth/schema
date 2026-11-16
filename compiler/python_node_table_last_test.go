package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonNodeTableLastThroughDriver(t *testing.T) {
	slowtest.Gate(t, "public Python compiler and pinned readers")
	u := unitFromSource(t, `package mapdemo
fixed table Item { count int32 }
table Crews {
    members map[uint32][..2]*Item
    past int32 | was = "after"
}
`)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/map_crews.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(good) != 137 || good[72] != 0 {
		t.Fatal("map_crews fixture layout changed")
	}
	bad := append([]byte(nil), good[:72]...)
	bad = append(bad, 3, 17, 3)
	bad = append(bad, good[72:]...)
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
	probe := pyrun.ImportPrelude + `from pathlib import Path
from mapdemo import Crews, Refused
assert Crews.load(Path("good.bin").read_bytes()).save() == Path("good.bin").read_bytes()
try:
    Crews.load(Path("bad.bin").read_bytes())
except Refused as err:
    assert err.reason == "wire_damaged", err.reason
else:
    raise AssertionError("node table followed by field accepted")
`
	inputs := map[string][]byte{"good.bin": good, "bad.bin": bad, "probe.py": []byte(probe)}
	pyrun.Run(t, dir, "probe.py", inputs)
	pyrun.RunFloor(t, dir, "probe.py", inputs)
}
