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

func TestPythonCookMapBlobs(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookmapblobs
 table Root {
  blobs map[int32]*bytes
  texts map[int32]*string
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookmapblobs import BlobRef, InvalidValue, Root
from cookmapblobs._schema import cook
import sys
r = Root(); r.blobs = {1: b"data", 2: b"data", 3: None}; r.texts = {1: "text", 2: "text", 3: None}
print(r.save().hex())
for shared in (False, True):
    if shared:
        b = BlobRef(b"data"); s = BlobRef("text"); r.blobs = {1: b, 2: b, 3: None}; r.texts = {1: s, 2: s, 3: None}
        try: r.save()
        except InvalidValue as e: assert "blob_shared" in e.rule
        else: raise AssertionError("shared map blob wire was accepted")
    for order in (1,2):
        data = cook.cook(r, order)
        if (order == 1) == (sys.byteorder == "little"): cook.check_cook(Root, data)
        print(data.hex())
`
	baseline := make(map[string]string)
	for name, runner := range cookPins() {
		lines := strings.Split(strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))), "\n")
		baseline[name] = strings.Join(lines, "\n")
		if len(lines) != 5 {
			t.Fatalf("%s: expected wire and four cooks, got %q", name, lines)
		}
		wire, err := hex.DecodeString(lines[0])
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		root := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if err := decodeCookWire(m, root, wire, &report); err != nil || report.Refused {
			t.Fatalf("%s: wire refused: %v %s", name, err, report.Reason)
		}
		for shared := range 2 {
			if shared == 1 {
				for i := range root.Fields {
					entries := root.Fields[i].Entries
					if len(entries) != 3 {
						t.Fatalf("expected three entries: %s", root.Fields[i].Def.Name)
					}
					entries[1].Tab.Fields[1].Cell.Blob = entries[0].Tab.Fields[1].Cell.Blob
				}
			}
			for i, big := range []bool{false, true} {
				want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				if lines[1+shared*2+i] != hex.EncodeToString(want) {
					t.Errorf("%s shared=%t order=%d: cook differs", name, shared == 1, i+1)
				}
			}
		}
	}
	t.Run("shared-map-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookmapblobs/_schema/cookwrite.py"
		const anchor = "identity = id(target_value) if isinstance(target_value, BlobRef) else None"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("map identity control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "identity = None", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for name, runner := range cookPins() {
			got := strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
			if got == baseline[name] {
				t.Errorf("%s: lost map blob identity was not detected", name)
			}
		}
	})

}
