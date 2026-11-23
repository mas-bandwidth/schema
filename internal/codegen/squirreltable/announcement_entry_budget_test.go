package squirreltable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "Probe.schema")
	writeFile(t, source, []byte("package entrybudget\n\nfixed table Probe { x int32 }\n"))
	one := luarun.LoadUnit(t, source)
	empty := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "blobs"))
	if len(tablewire.MessageVocabulary(empty)) != 0 || len(tablewire.MessageVocabulary(one)) != 1 {
		t.Fatal("expected ordinary empty and one-entry vocabularies")
	}
	inputs := map[string][]byte{"empty": tablewire.Announce(empty), "one": tablewire.Announce(one)}
	for name, data := range squirrelMessageFiles(t, one, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), data)
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte("local T = dofile(\"schema_module.nut\", true).require(\""+squirreltable.ModuleName(one)+"\");\n"+`local inputs = dofile("inputs.nut", true);
function check(data, size, maxEntries, maxBytes, accept) {
  local v = {}, report = {};
  if (maxEntries != null) v.MaxEntries <- maxEntries;
  if (maxBytes != null) v.MaxBytes <- maxBytes;
  local ok = T.AnnounceRead(v, data, report);
  assert(ok == accept, "entry limit " + (maxEntries == null ? "unset" : maxEntries.tostring()) + ", byte limit " + (maxBytes == null ? "unset" : maxBytes.tostring()) + ", size " + size);
  if (accept) {
    assert(v.Announced && v.Count == size, "accepted vocabulary count");
  } else {
    assert(!("Announced" in v && v.Announced) && report.reason == "vocabulary_too_large", "vocabulary limit refusal");
  }
}
foreach (sample in [[inputs["empty"], 0], [inputs["one"], 1]]) {
  local data = sample[0], size = sample[1];
  check(data, size, null, null, true);
  check(data, size, 0, null, size == 0);
  check(data, size, 1, null, true);
  check(data, size, null, 0, size == 0);
}
`))
	sqrun.Run(t, dir, "main.nut", inputs)
}
