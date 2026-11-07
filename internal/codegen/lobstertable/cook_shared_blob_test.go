package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The shared-blob fixture is legal as an in-memory region and as a cook
// (§2.5), while every wire read and wire/JSON writer refuses its sharing.
// Build it directly so the cook test never depends on a recovering reader.
const cookSharedBlobRecord = `def shared_catalog() -> schema_rt.Record?:
    let rec = schema_rt.new_record(schema_rt.table_of("Catalog"))
    let bytes = read_file("payload")
    assert bytes
    let node = schema_rt.Node { kind: 3, rec: nil, blob: bytes }
    for(rec.fields) sl:
        if sl.fd.name == "name":
            sl.cell.s = "twin"
            sl.count = 4
        elif sl.fd.name == "thumb" or sl.fd.name == "alias":
            sl.cell.blob = bytes
            sl.cell.blob_node = node
    return rec
`

func TestLobsterCookPreservesSharedBlobIdentity(t *testing.T) {
	t.Parallel()
	var row cookWriteRow
	for _, candidate := range cookWriteRows(t) {
		if candidate.name == "blob_shared" {
			row = candidate
			break
		}
	}
	if row.name == "" {
		t.Fatal("manifest has no shared-blob cook witness")
	}
	g := generateCookUnit(t, row)
	inputs := map[string][]byte{"payload": {0x10, 0x20, 0x30, 0x40}}
	for name, path := range map[string]string{"wire": row.wire, "little": row.little, "big": row.big} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString("import .json\nimport .schema_save\n")
	program.WriteString(cookSharedBlobRecord)
	fmt.Fprintf(&program, `
let wire = read_file("wire")
let little = read_file("little")
let big = read_file("big")
assert wire and little and big
assert not schema_rt.load("Catalog", wire, schema_rt.LoadReport {})
let rec = shared_catalog()
assert rec
let saved, save_reason = schema_rt.save_reasoned(rec)
assert saved == "" and save_reason == "blob_shared"
let text, text_reason = schema_rt.json_write(rec)
assert text == "" and text_reason != ""
assert %s.CatalogCookMeasure(rec) == length(little)
assert %s.CatalogCook(rec, false) == little
assert %s.CatalogCook(rec, true) == big
for([little, big]) cooked:
    let back = %s.CatalogReadCook(cooked)
    assert back
    let out, why = schema_rt.save_reasoned(back)
    let json, json_why = schema_rt.json_write(back)
    print(if out == "" and why == "blob_shared" and json == "" and json_why != "": "shared" else: "lost")
    schema_rt.release(back)
schema_rt.release(rec)
`, g.pkg, g.pkg, g.pkg, g.pkg)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "shared\nshared\n" {
		t.Fatalf("ReadCook lost shared-blob identity: %q", out)
	}
	path := filepath.Join(g.dir, "cook.lobster")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "            sl.cell.blob_node = c.blob_nodes[slot]\n"
	if strings.Count(string(raw), anchor) != 1 {
		t.Fatal("blob identity mutation anchor must occur once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), anchor, "", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); out != "lost\nlost\n" {
		t.Fatalf("negative control did not expose identity loss: %q", out)
	}
}
