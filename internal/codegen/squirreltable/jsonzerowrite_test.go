package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelJsonWriterRefusesZeroByte holds the Squirrel JSON writer to
// SPEC-TABLES §16.3 and W0/W8: zero bytes in stored text have no JSON
// spelling that this reader accepts, so ToJson refuses the whole value and
// ToJsonMeasure returns -1. The positive controls use U+0001, which remains
// writable as a JSON escape.
func TestSquirrelJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "squirrel")

	const src = `package probe
table T
{
    s string(8)
    ts [..2]string(8)
    mp map[string(8)]int32
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "ZeroWrite.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	typeFiles, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	tableFiles, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("generate tables: %v", err)
	}
	probeDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range typeFiles {
		writeFile(t, filepath.Join(probeDir, name), data)
	}
	for name, data := range tableFiles {
		writeFile(t, filepath.Join(probeDir, name), data)
	}

	probe := fmt.Sprintf(jsonZeroWriteProbe, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	output := string(sqrun.Run(t, dir, "main.nut", nil))
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 6 {
		t.Fatalf("probe printed %d rows, want 6:\n%s", len(lines), output)
	}
	for i := 0; i < len(lines); i += 2 {
		bad := strings.Fields(lines[i])
		good := strings.Fields(lines[i+1])
		if len(bad) != 4 || bad[1] != "bad" || bad[2] != "null" || bad[3] != "-1" {
			t.Errorf("%s: zero-byte write result %q, want ToJson null and ToJsonMeasure -1", lines[i], lines[i])
		}
		if len(good) != 5 || good[1] != "good" || good[2] != "text" || good[3] != good[4] {
			t.Errorf("%s: U+0001 control result %q, want text with matching measure", lines[i], lines[i+1])
		}
	}
}

const jsonZeroWriteProbe = `local schema = dofile("probe/schema_module.nut", true);
local M = schema.require(%q);
local J = schema.require("schema_table_json");
local members = {};
local textListType = null;
local mapEntry = null, mapKeyMember = null, mapValueMember = null;
foreach (fd in M.Descriptors.T.fields) {
  members[fd.name] <- fd.member;
  if (fd.name == "ts") textListType = fd.elem.table;
  if (fd.name == "mp") {
    mapEntry = fd.entry;
    mapKeyMember = fd.mapkey.member;
    mapValueMember = fd.mapvalue.member;
  }
}
local textList = function(value) {
  local elem = M.Descriptors[textListType].make();
  elem.Value = value;
  return [elem];
};
local mapOf = function(key) {
  local entry = M.Descriptors[mapEntry].make();
  entry[mapKeyMember] = key;
  entry[mapValueMember] = 1;
  return [entry];
};
local cases = [
  ["string", "s", "a\x00b", "a\x01b"],
  ["text_list", "ts", textList("a\x00b"), textList("a\x01b")],
  ["map_key", "mp", mapOf("a\x00b"), mapOf("a\x01b")]
];
foreach (c in cases) {
  local bad = M.Descriptors.T.make();
  bad[members[c[1]]] = c[2];
  local good = M.Descriptors.T.make();
  good[members[c[1]]] = c[3];
  try {
    local badText = J.ToJson(M, "T", bad);
    local badMeasure = J.ToJsonMeasure(M, "T", bad);
    local goodText = J.ToJson(M, "T", good);
    local goodMeasure = J.ToJsonMeasure(M, "T", good);
    print(c[0] + " bad " + (badText == null ? "null" : "text") + " " + badMeasure + "\n");
    print(c[0] + " good " + (goodText == null ? "null" : "text") + " " + goodMeasure +
      " " + (goodText == null ? "-1" : goodText.len()) + "\n");
  } catch (e) {
    print(c[0] + " raised " + e + "\n");
    print(c[0] + " good failed\n");
  }
}
`
