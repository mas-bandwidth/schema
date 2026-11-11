package jstable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJsNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (reserved node type ids)")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	files := make(map[string][]byte)
	for _, name := range []string{
		"node_type_ref_zero.bin",
		"node_type_node_table_id.bin",
		"node_type_build_version_id.bin",
		"node_type_vocabulary_id.bin",
		"node_chain_two.bin",
	} {
		b, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/framing", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	jsDiscardRun(t, u, files, `
for (const name of [
  "node_type_ref_zero.bin",
  "node_type_node_table_id.bin",
  "node_type_build_version_id.bin",
  "node_type_vocabulary_id.bin",
]) {
  const r = new m.TableVariableReport();
  const v = m.ListNodeLoad(new Uint8Array(readFileSync(name)), r);
  if (v !== null || r.err() === null) { fail(name + ": reserved type id was accepted"); }
}
{
  const r = new m.TableVariableReport();
  const v = m.ListNodeLoad(new Uint8Array(readFileSync("node_chain_two.bin")), r);
  if (v === null || r.err() !== null) { fail("node_chain_two: control wire did not load"); }
}
`)
}
