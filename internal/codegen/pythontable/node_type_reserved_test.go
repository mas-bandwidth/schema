package pythontable

import (
	"encoding/binary"
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

func TestPythonReservedNodeType(t *testing.T) {
	slowtest.Gate(t, "pinned Python node record type IDs and Go wire oracle")
	checkReservedNodeType(t, false)
}

func TestPythonReservedNodeTypeNegativeControl(t *testing.T) {
	slowtest.Gate(t, "pinned Python node record type fault control")
	checkReservedNodeType(t, true)
}

func checkReservedNodeType(t *testing.T, plant bool) {
	t.Helper()
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "tables/lists/Shared.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadNamedUnit(t, "Shared.schema", string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if plant {
		const old = "if type_id in (NODE_WIRE_ID, _BUILD_VERSION_WIRE_ID, _MESSAGE_VOCABULARY_WIRE_ID):"
		found := 0
		for name, data := range files {
			if strings.HasSuffix(name, "/_schema/load.py") && strings.Contains(string(data), old) {
				files[name] = []byte(strings.Replace(string(data), old, "if False and type_id in (NODE_WIRE_ID, _BUILD_VERSION_WIRE_ID, _MESSAGE_VOCABULARY_WIRE_ID):", 1))
				found++
			}
		}
		if found != 1 {
			t.Fatalf("could not plant exactly one node-type check: %d", found)
		}
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/list_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(golden) != 77 || golden[13] != 3 || binary.LittleEndian.Uint64(golden[37:45]) != ^uint64(0) {
		t.Fatal("list_shared fixture layout changed")
	}
	type vector struct {
		name   string
		wire   []byte
		reason string
	}
	mutateID := func(id uint64) []byte {
		b := append([]byte(nil), golden...)
		binary.LittleEndian.PutUint64(b[45:53], id)
		return b
	}
	node := append([]byte(nil), golden...)
	node[13] = 2
	none := append([]byte(nil), golden...)
	none[13] = 0
	cases := []vector{{"legal", golden, ""}, {"reserved_node", node, "wire_damaged"}, {"reserved_build", mutateID(^uint64(1)), "wire_damaged"}, {"reserved_vocab", mutateID(^uint64(2)), "wire_damaged"}, {"hash_zero", mutateID(0), ""}, {"type_ref_none", none, "wire_damaged"}}
	dir := t.TempDir()
	writeTree(t, dir, files)
	var manifest strings.Builder
	model := tabletext.NewModel(u)
	for _, c := range cases {
		var rep tabletext.Report
		ok, e := tablewire.Decode(model, model.New(model.Lookup("Album")), c.wire, &rep)
		reason := ""
		if !ok {
			reason = tablewire.RefusalReason(e)
		}
		if ok != (c.reason == "") || reason != c.reason {
			t.Fatalf("Go oracle %s: ok=%v, got %q, expected %q (%v)", c.name, ok, reason, c.reason, e)
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), c.wire, 0600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&manifest, "%s %s\n", c.name, c.reason)
	}
	probe := pyrun.ImportPrelude + `from pathlib import Path
from listdemo import Album, Refused
from listdemo._schema import LoadReport
for line in Path("cases.txt").read_text().splitlines():
    parts = line.split()
    name, reason = parts[0], parts[1] if len(parts) > 1 else ""
    report = LoadReport()
    report.unknown, report.widened, report.retained, report.retain_lost = 77, 66, 55, 44
    try:
        value = Album.load(Path(name + ".bin").read_bytes(), report=report)
    except Refused as err:
        assert err.reason == reason, (name, err.reason, reason)
        assert (report.unknown, report.widened, report.retained, report.retain_lost) == (77, 66, 55, 44)
    else:
        assert not reason, (name, "accepted", reason)
        if name == "legal":
            assert value.photos[2] is None
            assert value.save() == Path(name + ".bin").read_bytes()
print("node-type PASS")
`
	inputs := map[string][]byte{"probe.py": []byte(probe), "cases.txt": []byte(manifest.String())}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "probe.py", inputs)
		if plant {
			if err == nil || !strings.Contains(stderr, "reserved_node', 'accepted'") {
				t.Fatalf("node type fault not detected by %s: %v\n%s%s", pin, err, out, stderr)
			}
		} else if err != nil || !strings.Contains(out, "node-type PASS") {
			t.Fatalf("node type probe failed under %s: %v\n%s%s", pin, err, out, stderr)
		}
	}
}
