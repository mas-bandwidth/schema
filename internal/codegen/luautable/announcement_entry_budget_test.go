package luautable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(source, []byte("package entrybudget\n\nfixed table Probe { x int32 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	one := review1121Unit(t, source)
	empty := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "blobs"))
	if len(tablewire.MessageVocabulary(empty)) != 0 || len(tablewire.MessageVocabulary(one)) != 1 {
		t.Fatal("expected ordinary empty and one-entry vocabularies")
	}
	inputs := map[string][]byte{"empty": tablewire.Announce(empty), "one": tablewire.Announce(one)}
	probe := filepath.Join(dir, "probe.luau")
	if err := os.WriteFile(probe, []byte(`local inputs = require("./inputs")
local M = require("./schema_message")
local function check(data, size, maxEntries, maxBytes, accept)
  local v = M.NewVocabulary()
  local report = {}
  if maxEntries ~= nil then v.maxEntries = maxEntries end
  if maxBytes ~= nil then v.maxBytes = maxBytes end
  local ok = M.AnnounceRead(v, data, report)
  assert(ok == accept, "entry limit " .. tostring(maxEntries) .. ", byte limit " .. tostring(maxBytes) .. ", size " .. tostring(size))
  if accept then
    assert(v.announced and v.count == size, "accepted vocabulary count")
  else
    assert(not v.announced and report.reason == "vocabulary_too_large", "vocabulary limit refusal")
  end
end
for _, sample in ipairs({{inputs.empty, 0}, {inputs.one, 1}}) do
  local data, size = sample[1], sample[2]
  check(data, size, nil, nil, true)
  check(data, size, 0, nil, size == 0)
  check(data, size, 1, nil, true)
  check(data, size, nil, 0, size == 0)
end
`), 0o644); err != nil {
		t.Fatal(err)
	}
	luaurun.Run(t, review1121Modules(t, one, probe), "main.luau", inputs)
}
