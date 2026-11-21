package pythontable

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
	// The verdict is the contract (docs/rulings.json, refusal-verdict-only):
	// why a read refused is each side's own diagnostic, never compared.
	type vector struct {
		name    string
		wire    []byte
		refused bool
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
	cases := []vector{{"legal", golden, false}, {"reserved_node", node, true}, {"reserved_build", mutateID(^uint64(1)), true}, {"reserved_vocab", mutateID(^uint64(2)), true}, {"hash_zero", mutateID(0), false}, {"type_ref_none", none, true}}
	dir := t.TempDir()
	writeTree(t, dir, files)
	var manifest strings.Builder
	model := tabletext.NewModel(u)
	for _, c := range cases {
		var rep tabletext.Report
		ok, e := tablewire.Decode(model, model.New(model.Lookup("Album")), c.wire, &rep)
		if !ok && !tablewire.ReadRefusal(e) {
			t.Fatalf("Go oracle %s: fails with %v, which is not a read's refusal", c.name, e)
		}
		if ok == c.refused {
			t.Fatalf("Go oracle %s: ok=%v, expected refused=%v (%v)", c.name, ok, c.refused, e)
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), c.wire, 0600); err != nil {
			t.Fatal(err)
		}
		verdict := "accepted"
		if c.refused {
			verdict = "refused"
		}
		fmt.Fprintf(&manifest, "%s %s\n", c.name, verdict)
	}
	probe := pyrun.ImportPrelude + `from pathlib import Path
from listdemo import Album, Refused
from listdemo._schema import LoadReport
for line in Path("cases.txt").read_text().splitlines():
    name, verdict = line.split()
    report = LoadReport()
    report.unknown, report.widened, report.retained, report.retain_lost = 77, 66, 55, 44
    try:
        value = Album.load(Path(name + ".bin").read_bytes(), report=report)
    except Refused as err:
        assert verdict == "refused", (name, "refused", err.reason)
        assert (report.unknown, report.widened, report.retained, report.retain_lost) == (77, 66, 55, 44)
    else:
        assert verdict == "accepted", (name, "accepted", verdict)
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
