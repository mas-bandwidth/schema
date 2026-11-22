package luatable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A fixed array whose bound grew since the writer (docs/SPEC-TABLES.md §21.1,
// "a grown bound") reads as the writer's elements followed by one fresh
// default element per new slot. Each row below has the Go engine write
// `[2]T` and Lua read it as `[4]T` in the message form (F-092: before the fix
// the new slots were left nil and the re-save died on them) and in the fixed
// form, then save the value as a message batch; the bytes must be the
// engine's own read of the same batch, re-encoded.

type luaWidenBoundKind struct {
	name     string
	decls    string
	elem     string
	two      string
	optional bool
}

var luaWidenBoundKinds = []luaWidenBoundKind{
	{name: "bool", elem: "bool", two: `[true,true]`},
	{name: "uint8", elem: "uint8", two: `[255,6]`},
	{name: "int32", elem: "int32", two: `[-5,6]`},
	{name: "float32", elem: "float32", two: `[1.5,-2.25]`},
	{name: "enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`},
	{name: "flags", decls: "flags Caps {\n Read\n Write\n}\n", elem: "Caps", two: `[["Read"],["Read","Write"]]`},
	{name: "record", decls: "fixed table Inner {\n v int32 = 7\n s string(8) = \"born\"\n}\n", elem: "Inner", two: `[{"v":1,"s":"a"},{"v":2,"s":"b"}]`},
	{name: "union", decls: "fixed table Inner {\n v int32\n}\nunion Choice {\n count int32\n nested Inner\n}\n", elem: "Choice", two: `[{"count":5},{"nested":{"v":3}}]`},
	{name: "optional-int32", elem: "int32", two: `[-5,6]`, optional: true},
	{name: "optional-enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`, optional: true},
}

func luaWidenBoundSchema(k luaWidenBoundKind, bound int) string {
	opt := ""
	if k.optional {
		opt = "?"
	}
	return fmt.Sprintf("package widenbound\n%sfixed table Root {\n items %s[%d]%s\n}\n", k.decls, opt, bound, k.elem)
}

func luaWidenBoundUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "widenbound.schema")
	writeFile(t, path, src)
	return luarun.LoadUnit(t, path)
}

const luaWidenBoundProbe = `local T = require(%q)
local inputs = require("inputs")
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.peer, {}), "the peer's announcement was refused")
local function held(label, v)
  local n = v.items and #v.items
  if n ~= 4 then error(label .. ": " .. tostring(n) .. " slots, not 4", 0) end
  for i = 3, 4 do
    if v.items[i] == nil then error(label .. ": slot " .. i .. " is nil", 0) end
  end
  local ok, saved = pcall(T.SaveMessages, "Root", vocab, { v })
  if not ok then error(label .. ": the re-save failed: " .. tostring(saved), 0) end
  if saved ~= inputs.want then error(label .. ": the re-saved batch is not the engine's", 0) end
end
local values = T.LoadMessages("Root", vocab, inputs.message, {})
if values == nil or #values ~= 1 then error("message: the batch was refused", 0) end
held("message", values[1])
local ok, rows = pcall(T.FixedLoad, "Root", inputs.fixed)
if not ok or rows == nil or #rows ~= 1 then error("fixed: the file was refused: " .. tostring(rows), 0) end
held("fixed", rows[1])
print("ok")
`

func TestLuaWidenedFixedArrayBoundPadsOneDefaultPerSlot(t *testing.T) {
	t.Parallel()
	for _, k := range luaWidenBoundKinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			older := luaWidenBoundUnit(t, luaWidenBoundSchema(k, 2))
			newer := luaWidenBoundUnit(t, luaWidenBoundSchema(k, 4))
			writer, reader := tabletext.NewModel(older), tabletext.NewModel(newer)

			root := writer.New(writer.Lookup("Root"))
			var rep tabletext.Report
			if !writer.Read(root, []byte(`{"items":`+k.two+`}`), &rep) || rep.Refused {
				t.Fatalf("fixture %s: %+v", k.two, rep)
			}
			peer := tablewire.Announce(older)
			message, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{root})
			if err != nil {
				t.Fatal(err)
			}
			fixed, err := tablewire.EncodeFixed(writer, root)
			if err != nil {
				t.Fatal(err)
			}

			// THE REFERENCE: the Go engine reads the writer's batch under the
			// reader's declaration and re-encodes it.
			var vocab tablewire.Vocabulary
			if err := vocab.AnnounceRead(peer, &rep); err != nil {
				t.Fatal(err)
			}
			read := reader.New(reader.Lookup("Root"))
			if _, ok, err := tablewire.DecodeMessages(reader, []*tabletext.Instance{read}, message, &vocab, &rep); !ok || err != nil {
				t.Fatalf("the engine did not read across the grown bound: %v %+v", err, rep)
			}
			want, err := tablewire.EncodeMessages(reader, []*tabletext.Instance{read})
			if err != nil {
				t.Fatal(err)
			}

			entry, ok := FixedLineageOf(older, "Root")
			if !ok {
				t.Fatal("the writer's fixed layout is missing")
			}
			files, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"Root": {entry}})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			luarun.WriteFiles(t, dir, map[string][]byte{"main.lua": fmt.Appendf(nil, luaWidenBoundProbe, ModuleName(newer))})
			stdout, stderr, err := luarun.Try(t, dir, "main.lua", map[string][]byte{
				"peer": peer, "message": message, "fixed": fixed, "want": want,
			})
			if err != nil || strings.TrimSpace(stdout) != "ok" {
				t.Fatalf("widened bound: %v\n%s%s", err, stdout, stderr)
			}
		})
	}
}
