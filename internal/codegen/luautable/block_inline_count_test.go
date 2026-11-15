package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const blockInlineCountSchema = `package probe
fixed table Crew {
  id uint64
}
fixed table Ship {
  id uint64
  crew [..2]Crew
}
fixed table Root {
  stamp uint64
  ships [..2]Ship
}
`

// TestLuauBlockInlineCountBounded holds a block read to its record
// (docs/SPEC-TABLES.md §19.2): an inline counted array's count past its
// declared bound is refused, the out-of-line arrays of a nested record are
// held to the same bound as the root's, and a clean block still reads the
// rows the generated Go builder wrote.
func TestLuauBlockInlineCountBounded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the generated Go oracle")
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schema, []byte(blockInlineCountSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	blocks := ir.Blocks(u)

	// the generated Go builder writes the block and the generated Go reader
	// answers the rows a clean read must land
	goDir := filepath.Join(dir, "go")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goFiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	more, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, more)
	runtime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	goFiles["probe_test.go"] = blockInlineCountProbe(dir)
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runBlockProbe(t, goDir, "TestBuild")
	block, err := os.ReadFile(filepath.Join(dir, "block.bin"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(dir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// the Luau leg: the emitted modules, the package runtime, and the same
	// unit's descriptor graph
	luDir := filepath.Join(dir, "luau")
	if err := os.MkdirAll(luDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	files["descriptor.luau"] = []byte(luauBlockDescriptor(u, blocks))
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(luDir, "main.luau"), []byte(blockInlineCountRead), 0o644); err != nil {
		t.Fatal(err)
	}
	got := luaurun.Run(t, luDir, "main.luau", map[string][]byte{"block": block})
	want := string(expected) + "ships-over-count refused-open\n" +
		"crew-over-count refused-read\ncrew-far-count refused-read\n"
	if string(got) != want {
		t.Fatalf("the block read does not hold its record:\nLua:\n%s\nwant:\n%s", got, want)
	}
}

// blockInlineCountRead is the Luau probe: it reads the clean block, then
// forges the root's out-of-line count and a nested row's counted array one
// past their declared maxima, and finally a nested count far past it. Each
// forgery must answer a refusal rather than a value or a raised error.
const blockInlineCountRead = `local B = require("./schema_blockread")
local D = require("./descriptor")
local I = require("./inputs")

local blk, why = B.BlockOpen(D.root, I.block, 0, buffer.len(I.block))
assert(blk ~= nil, "the clean block does not open: " .. tostring(why))
local p, rwhy = B.Read(D.root, blk)
assert(p ~= nil, "the clean block does not read: " .. tostring(rwhy))
print("stamp " .. tostring(p.stamp))
for i, s in p.ships do
	print(string.format("ship %d %d %d", i - 1, s.id, #s.crew))
	for j, c in s.crew do
		print(string.format("crew %d %d %d", i - 1, j - 1, c.id))
	end
end

local ships
for _, f in D.root.fields do
	if f.outOfLine then ships = f end
end
assert(ships ~= nil, "the root descriptor names no out-of-line array")
local function rowBase(i: number): number
	return integer.tonumber(buffer.readinteger(I.block, ships.offsetOfOffset)) + i * buffer.readu32(I.block, ships.strideOffset)
end
local crew
for _, f in ships.element.fields do
	if f.isArray and f.element ~= nil and f.countOffset ~= nil then crew = f end
end
assert(crew ~= nil, "the row descriptor names no counted array")

local function copy(): buffer
	local b = buffer.create(buffer.len(I.block))
	buffer.copy(b, 0, I.block, 0, buffer.len(I.block))
	return b
end
local function attempt(name: string, b: buffer)
	local ok, h, openWhy = pcall(function() return B.BlockOpen(D.root, b, 0, buffer.len(b)) end)
	if not ok then print(name .. " threw-open") return end
	if h == nil then print(name .. " refused-open") return end
	local rok, v, readWhy = pcall(function() return B.Read(D.root, h) end)
	if not rok then print(name .. " threw-read") return end
	if v == nil then print(name .. " refused-read") else print(name .. " read") end
end

local b = copy()
buffer.writeu32(b, ships.countOffset, ships.bound + 1)
attempt("ships-over-count", b)
b = copy()
buffer.writeu32(b, rowBase(0) + crew.countOffset, crew.arrayBound + 1)
attempt("crew-over-count", b)
b = copy()
buffer.writeu32(b, rowBase(0) + crew.countOffset, 0x7fffffff)
attempt("crew-far-count", b)
`

// blockInlineCountProbe is the generated unit's own program: TestBuild fills
// the block through the generated builder, writes it, and writes the rows the
// generated reader lands for it.
func blockInlineCountProbe(dir string) []byte {
	return []byte(fmt.Sprintf(`package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

const outDir = %q

func TestBuild(t *testing.T) {
	var storage RootBlockStorage
	if !storage.Create(TableBlockDefaultAllocator()) {
		t.Fatal("create")
	}
	defer storage.Destroy()
	var block RootBlock
	if !RootBlockBegin(&block, &storage, RootCounts{Ships: 2}, nil) {
		t.Fatal("begin")
	}
	block.Projection.Stamp = 4242
	for i := int32(0); i < 2; i++ {
		r := block.Ships().At(i)
		r.ID = uint64(100 + i)
		r.CrewCount = 2
		r.Crew[0].ID = uint64(1000 + i*10)
		r.Crew[1].ID = uint64(2000 + i*10)
	}
	raw := unsafe.Slice((*byte)(block.Base), block.Bytes)
	if err := os.WriteFile(filepath.Join(outDir, "block.bin"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var got RootBlock
	if !RootBlockOpen(&got, block.Base, block.Bytes) {
		t.Fatal("open built block")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "stamp %%d\n", got.Projection.Stamp)
	for i := int32(0); i < got.Ships().Len(); i++ {
		r := got.Ships().At(i)
		fmt.Fprintf(&out, "ship %%d %%d %%d\n", i, r.ID, r.CrewCount)
		for j := int32(0); j < r.CrewCount; j++ {
			fmt.Fprintf(&out, "crew %%d %%d %%d\n", i, j, r.Crew[j].ID)
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "expected.txt"), []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`, dir))
}
