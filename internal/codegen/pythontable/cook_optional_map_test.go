package pythontable

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func TestPythonCookOptionalMaps(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, optionalMapSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from optmap import Leaf, Root
from optmap._schema import cook
import sys
empty = Root(); empty.tags = {}; empty.nodes = {}
full = Root(); leaf = Leaf(); leaf.value = 17; full.tags = {"z": 3, "a": -7}; full.nodes = {1: leaf, 2: None, 3: leaf}
for r in (Root(), empty, full):
    print(r.save().hex())
    for order in (1,2):
        data = cook.cook(r, order)
        if (order == 1) == (sys.byteorder == "little"):
            cook.check_cook(Root, data)
        print(data.hex())
`
	for name, runner := range cookPins() {
		lines := strings.Split(strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))), "\n")
		if len(lines) != 9 {
			t.Fatalf("%s: expected three wire/cook triples, got %q", name, lines)
		}
		for caseIndex := range 3 {
			wire, err := hex.DecodeString(lines[caseIndex*3])
			if err != nil {
				t.Fatal(err)
			}
			m := tabletext.NewModel(u)
			root := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if err := decodeCookWire(m, root, wire, &report); err != nil {
				t.Fatal(err)
			}
			if report.Refused {
				t.Fatalf("%s: optional map wire refused: %s", name, report.Reason)
			}
			for i, big := range []bool{false, true} {
				want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				if lines[caseIndex*3+i+1] != hex.EncodeToString(want) {
					t.Errorf("%s case %d order %d: optional map cook differs", name, caseIndex, i+1)
				}
			}
		}
	}

	t.Run("map-presence-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "optmap/_schema/cookwrite.py"
		const anchor = "buf[base + fd.pieces[-1][0]] = 1"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("presence control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "buf[base + fd.pieces[-1][0]] = 0", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "absent optional has nonzero payload") {
				t.Errorf("%s: presence control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})

}
