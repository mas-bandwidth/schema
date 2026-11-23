package mootable_test

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudget(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	g := generateFiles(t, map[string]string{"Probe.schema": "package entrybudget\n\nfixed table Probe { x int32 }\n"})
	empty := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "blobs"))
	if len(tablewire.MessageVocabulary(empty)) != 0 || len(tablewire.MessageVocabulary(g.unit)) != 1 {
		t.Fatal("expected ordinary empty and one-entry vocabularies")
	}
	inputs := map[string][]byte{"empty": tablewire.Announce(empty), "one": tablewire.Announce(g.unit)}
	g.run(t, `local inputs = require("./inputs")
local MS = require("./table_message")
local function check(data, size, maxEntries, maxBytes, accept)
  local v = MS.NewVocabulary()
  local report = {}
  if maxEntries ~= nil then v.maxEntries = maxEntries end
  if maxBytes ~= nil then v.maxBytes = maxBytes end
  local ok = MS.AnnounceRead(v, data, report)
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
`, inputs)
}
