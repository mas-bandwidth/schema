package mootable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonDepthCapSchema puts a table one level inside each kind of container the
// JSON read opens: a map's value, a union's body arm and a list's element.
const jsonDepthCapSchema = `package depthcap

type Inner
{
    n int32
}

union Arm
{
    body Inner
    idle
}

table Root
{
    arm  Arm
    tags map[string]Inner
    list [..4]Inner
}
`

// TestMooJsonDepthCap holds the JSON read to §16.2's depth cap, which counts
// every container, placed or skipped, the root at depth 0: a text whose
// deepest container is at depth 128 reads and one at 129 refuses. Each row
// reaches its table through a different container (the map object, the union
// object, the list) and nests arrays under an unknown key there, at depth 3,
// so a reader that counts the map or the union as no level reads the 129 row
// (testdata/conformance/tables MANIFEST.txt, depth-map-past-cap and
// depth-union-past-cap).
func TestMooJsonDepthCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted text form)")

	nest := func(k int) string { return strings.Repeat("[", k) + strings.Repeat("]", k) }
	sites := []struct{ name, open, close string }{
		{"map", `{"tags": {"a": {"zz": `, `}}}`},
		{"union", `{"arm": {"body": {"zz": `, `}}}`},
		{"list", `{"list": [{"zz": `, `}]}`},
	}
	type row struct{ name, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for _, s := range sites {
		// the unknown key's value opens at depth 3, so k arrays reach 2 + k
		for _, c := range []struct {
			k       int
			verdict string
		}{{126, "read"}, {127, "refused"}} {
			name := fmt.Sprintf("%s-%d", s.name, 2+c.k)
			inputs[name] = []byte(s.open + nest(c.k) + s.close)
			rows = append(rows, row{name, c.verdict})
		}
	}

	u := unitFromSource(t, jsonDepthCapSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	mod := u.DeclFile["Root"] + "Table"
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\n")
	fmt.Fprintf(&b, "local M: any = require(\"./%s\")\n", mod)
	b.WriteString("local function run(name: string)\n")
	b.WriteString("\tlocal v = M.NewRoot()\n\tlocal r: any = {}\n")
	b.WriteString("\tlocal ok = M.RootFromJson(v, inputs[name], r)\n")
	b.WriteString("\tprint(name .. \" \" .. (if ok then \"read\" else \"refused\"))\nend\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "run(%q)\n", r.name)
	}
	out := strings.Split(g.run(t, b.String(), inputs), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the probe printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		if want := r.name + " " + r.verdict; out[i] != want {
			t.Errorf("got %q, want %q", out[i], want)
		}
	}
}

// jsonWriteDepthSchema is internal/tabletext's depthWriteUnit: a pointer chain
// whose every node holds one container the writer always emits, an EMPTY one
// at that: an array, a union, a map, an enum-keyed array and a flags value.
const jsonWriteDepthSchema = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

// TestMooJsonWriteDepthCapCountsEveryContainer holds the same cap on the
// write (§16.14 W2), as internal/tabletext's
// TestWriteDepthCapCountsEveryContainer holds the Go one: 127 nodes under the
// root put the last node's container at depth 128, which writes and reads
// back; 128 nodes put it at 129, which the writer refuses, because its own
// reader refuses that text.
func TestMooJsonWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted text form)")

	u := unitFromSource(t, jsonWriteDepthSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	var b strings.Builder
	b.WriteString("--!strict\n")
	fmt.Fprintf(&b, "local M: any = require(\"./%s\")\n", u.DeclFile["FlagsNode"]+"Table")
	b.WriteString(`local function chain(name: string, n: number): any
	local root = M["New" .. name]()
	local cur = root
	for _ = 1, n do
		local nxt = M["New" .. name]()
		cur.Next = nxt
		cur = nxt
	end
	return root
end
for _, name in { "ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode" } do
	for _, n in { 127, 128 } do
		local v = chain(name, n)
		local size = M[name .. "ToJsonMeasure"](v)
		local wrote, read = 0, 0
		if size >= 0 then
			local out = buffer.create(size)
			if M[name .. "ToJson"](v, out) == size then
				wrote = 1
				if M[name .. "FromJson"](M["New" .. name](), out, {}) then read = 1 end
			end
		end
		print(name .. " " .. tostring(n) .. " write=" .. tostring(wrote) .. " read=" .. tostring(read))
	end
end
`)
	got := g.run(t, b.String(), nil)
	want := strings.Join([]string{
		"ArrNode 127 write=1 read=1", "ArrNode 128 write=0 read=0",
		"UnionNode 127 write=1 read=1", "UnionNode 128 write=0 read=0",
		"MapNode 127 write=1 read=1", "MapNode 128 write=0 read=0",
		"KeyedNode 127 write=1 read=1", "KeyedNode 128 write=0 read=0",
		"FlagsNode 127 write=1 read=1", "FlagsNode 128 write=0 read=0",
	}, "\n")
	if got != want {
		t.Fatalf("the moo text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
