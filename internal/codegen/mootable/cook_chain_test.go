package mootable_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooCookReadsALongPointerChain: the length of a pointer chain is not a
// depth (docs/SPEC-TABLES.md §3.1). A Scene whose head chains this many
// ListNode records is saved and loaded on the variable form, cooked by the
// port's own writer, and read back through the cook reader; both values must
// agree node for node, however long the chain.
func TestMooCookReadsALongPointerChain(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookread.moo over a long pointer chain)")
	t.Parallel()
	const nodes = 30000
	units, _ := readManifest(t)
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	for _, file := range []string{"table_cookread.moo", "table_cookwrite.moo"} {
		if g.files[file] == nil {
			t.Fatalf("graphdemo emits no %s", file)
		}
	}
	main := fmt.Sprintf(`--!strict
local load = require("./table_load")
local save = require("./table_save")
local C = require("./table_cookread")
local W = require("./table_cookwrite")
local St = require("./schema_tables")
%s
local T = require("./%s")
local scene = T.NewScene()
local head: any = nil
for i = %d, 1, -1 do
	local node = T.NewListNode()
	node.Value = i
	node.Name = "n" .. tostring(i)
	node.Next = head
	head = node
end
scene.Head = head
local wire, whySave = save.save(St.lookup("Scene"), scene)
if wire == nil then print("save " .. whySave.name .. " " .. tostring(whySave.field)) return end
local loaded = load.load("Scene", wire, load.newReport())
if loaded == nil then print("load refused") return end
local cooked, whyCook = W.cook("Scene", loaded, false)
if cooked == nil then print("cook " .. whyCook.name .. " " .. tostring(whyCook.field)) return end
local h, whyOpen = C.open("Scene", cooked, 0, buffer.len(cooked))
if h == nil then print("open " .. tostring(whyOpen)) return end
local back, whyRead = C.read(h)
if back == nil then print("read " .. whyRead.name .. " " .. tostring(whyRead.field)) return end
local a, b = loaded.Head, back.Head
local i = 0
while a ~= nil and b ~= nil do
	i += 1
	if a.Value ~= b.Value or a.Name ~= b.Name then
		print("differs at " .. tostring(i))
		return
	end
	a = a.Next
	b = b.Next
end
if a ~= nil or b ~= nil then
	print("length differs after " .. tostring(i))
	return
end
print("ok " .. tostring(i))
`, g.requireAll(), tableModuleOf(g, "Scene"), nodes)
	out := g.run(t, main, nil)
	if want := fmt.Sprintf("ok %d", nodes); out != want {
		t.Fatalf("the cook read of a %d-node chain: got %q, want %q", nodes, out, want)
	}
}
