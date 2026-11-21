package luautable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math/big"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const blockReadSchema = `package probe
fixed table Ship {
 id uint64
 x int32
 flag bool
}
fixed table Root {
 stamp uint64
 ships [..4]Ship
 others [..2]Ship
}
`

// TestLuauBlockReadAgreesWithGo pins docs/SPEC-TABLES.md §19.2 and §19.4 on the
// Luau leg: the generated Go builder writes a block, both readers land the same
// records through their own descriptors, and each damaged block opens or
// refuses exactly where Go's BlockOpen does. Expected bytes and verdicts come
// from the generated Go of the same unit, never from a number typed here. A
// damaged block is held to Go's VERDICT only: the reason each side names is
// per-language diagnostic detail, never compared across targets
// (docs/rulings.json refusal-verdict-only).
func TestLuauBlockReadAgreesWithGo(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	luauBlockReadAgreesWithGo(t, nil)
}

// TestLuauBlockReadReasonIsDiagnostic renames every reason the Luau BlockOpen
// names: the verdicts are unchanged, so the agreement must stay green.
func TestLuauBlockReadReasonIsDiagnostic(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	got := luauBlockReadAgreesWithGo(t, func(t *testing.T, files map[string][]byte) {
		const anchor = `return nil, "`
		src := string(files["schema_blockread.luau"])
		if !strings.Contains(src, anchor) {
			t.Fatalf("NEGATIVE CONTROL FAILED: schema_blockread.luau names no refusal by %q", anchor)
		}
		files["schema_blockread.luau"] = []byte(strings.ReplaceAll(src, anchor, anchor+"renamed_by_the_plant_"))
	})
	if !strings.Contains(got, " refused renamed_by_the_plant_") {
		t.Errorf("NEGATIVE CONTROL FAILED: the rename reached no damaged block:\n%s", got)
	}
}

// blockVerdicts maps each damaged block's name to its verdict, "ok" or
// "refused"; anything after the verdict is that side's diagnostic.
func blockVerdicts(t *testing.T, side string, out []byte) map[string]string {
	t.Helper()
	verdicts := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[1] != "ok" && f[1] != "refused") || (f[1] == "ok" && len(f) != 2) {
			t.Fatalf("%s printed %q, not a case and its verdict", side, line)
		}
		verdicts[f[0]] = f[1]
	}
	return verdicts
}

// luauBlockReadAgreesWithGo runs both legs, the Luau files edited by plant
// first when it is not nil, and returns the Luau damaged-block lines.
func luauBlockReadAgreesWithGo(t *testing.T, plant func(*testing.T, map[string][]byte)) string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schema, []byte(blockReadSchema), 0o644); err != nil {
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

	// the generated Go unit: the builder writes the block, the generated
	// reader lands the expected records and verdicts
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
	goFiles["probe_test.go"] = blockReadProbe(dir)
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
	want, err := os.ReadFile(filepath.Join(dir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// the Luau leg: the generated unit's modules, the package runtime's
	// schema_blockread.luau, and a descriptor graph computed from the same IR
	luDir := filepath.Join(dir, "luau")
	if err := os.MkdirAll(luDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	blockread, ok := luautable.Runtime()["schema_blockread.luau"]
	if !ok {
		t.Fatal("the Luau table runtime emits no schema_blockread.luau")
	}
	files["schema_blockread.luau"] = blockread
	files["descriptor.luau"] = []byte(luauBlockDescriptor(u, blocks))
	if plant != nil {
		plant(t, files)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `--!strict
local B = require("./schema_blockread")
local D = require("./descriptor")
local I = require("./inputs")
local blk, reason = B.BlockOpen(D.root, I.block, 0, buffer.len(I.block))
if blk == nil then
	print("reason " .. (reason or "nil"))
else
	local p = B.Read(D.root, blk)
	print("stamp " .. tostring(p.stamp))
	for i, r in p.ships do
		print(string.format("ships %d %d %d %d", i - 1, r.id, r.x, r.flag and 1 or 0))
	end
	for i, r in p.others do
		print(string.format("others %d %d %d %d", i - 1, r.id, r.x, r.flag and 1 or 0))
	end
end
`
	if err := os.WriteFile(filepath.Join(luDir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := luaurun.Run(t, luDir, "main.luau", map[string][]byte{"block": block})
	if string(got) != string(want) {
		t.Fatalf("the Luau reflective read disagrees with Go's generated read:\nLua:\n%s\nGo:\n%s", got, want)
	}

	// damaged blocks: the bytes are Go's, and so is every verdict
	cases := blockReadDamage(t, block, blocks.Block("Root"))
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	var index strings.Builder
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, "cases", c.name), c.data, 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&index, "%s %d\n", c.name, c.base)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases", "index.txt"), []byte(index.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runBlockProbe(t, goDir, "TestCases")
	goReasons, err := os.ReadFile(filepath.Join(dir, "go_reasons.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var damageMain strings.Builder
	damageMain.WriteString("--!strict\nlocal B = require(\"./schema_blockread\")\nlocal D = require(\"./descriptor\")\nlocal I = require(\"./inputs\")\n")
	for _, c := range cases {
		fmt.Fprintf(&damageMain, "do\n local buf = I[%q]\n local blk, reason = B.BlockOpen(D.root, buf, %d, buffer.len(buf) - %d)\n print(%q .. (if blk == nil then \" refused \" .. tostring(reason) else \" ok\"))\nend\n", c.name, c.base, c.base, c.name)
	}
	if err := os.WriteFile(filepath.Join(luDir, "damage.luau"), []byte(damageMain.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		inputs[c.name] = c.data
	}
	gotReasons := luaurun.Run(t, luDir, "damage.luau", inputs)
	want, got := blockVerdicts(t, "Go's BlockOpen", goReasons), blockVerdicts(t, "the Luau BlockOpen", gotReasons)
	if len(want) != len(cases) || !maps.Equal(got, want) {
		t.Fatalf("the Luau block verdicts disagree with Go's BlockOpen (the reasons are diagnostic only):\nLua:\n%s\nGo:\n%s", gotReasons, goReasons)
	}
	return string(gotReasons)
}

type blockReadCase struct {
	name string
	data []byte
	base int
}

func blockReadDamage(t *testing.T, block []byte, bl *ir.BlockLayout) []blockReadCase {
	t.Helper()
	clone := func() []byte { return append([]byte(nil), block...) }
	first := bl.Arrays[0]
	put64 := func(b []byte, at int64, v uint64) { binary.LittleEndian.PutUint64(b[at:], v) }
	put32 := func(b []byte, at int64, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	var out []blockReadCase

	b := clone()
	b[0] ^= 0xff
	out = append(out, blockReadCase{"magic", b, 0})

	b = clone()
	put64(b, 0, bits.ReverseBytes64(ir.BlockMagic))
	out = append(out, blockReadCase{"foreign", b, 0})

	b = clone()
	b[16] = 2
	out = append(out, blockReadCase{"byteorder", b, 0})

	b = clone()
	b[8] ^= 0xff
	out = append(out, blockReadCase{"version", b, 0})

	b = clone()
	put32(b, first.StrideOffset, uint32(first.Stride)+1)
	out = append(out, blockReadCase{"stride", b, 0})

	b = clone()
	put32(b, first.CountOffset, uint32(first.Max)+1)
	out = append(out, blockReadCase{"count", b, 0})

	b = clone()
	put64(b, first.OffsetOfOffset, uint64(bl.Projection.Size)+1)
	out = append(out, blockReadCase{"offset", b, 0})

	truncated := clone()
	out = append(out, blockReadCase{"truncated", truncated[:len(truncated)-1], 0})

	unaligned := append([]byte{0}, block...)
	out = append(out, blockReadCase{"unaligned", unaligned, 1})
	return out
}

func runBlockProbe(t *testing.T, dir, name string) {
	t.Helper()
	cmd := testproc.Command(t, "nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^"+name+"$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go probe %s: %v\n%s", name, err, out)
	}
}

// blockReadProbe is the generated unit's own program: TestBuild fills the block
// through the generated builder and writes it, and TestCases opens each damaged
// file through the generated reader and writes its verdict.
func blockReadProbe(dir string) []byte {
	return []byte(fmt.Sprintf(`package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

const outDir = %q

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestBuild(t *testing.T) {
	var storage RootBlockStorage
	if !storage.Create(TableBlockDefaultAllocator()) {
		t.Fatal("create")
	}
	defer storage.Destroy()
	var block RootBlock
	if !RootBlockBegin(&block, &storage, RootCounts{Ships: 4, Others: 2}, nil) {
		t.Fatal("begin")
	}
	block.Projection.Stamp = 1234567890123
	for i := int32(0); i < 4; i++ {
		r := block.Ships().At(i)
		r.ID = uint64(1000 + i)
		r.X = int32(-7*i + 11)
		r.Flag = i%%2 == 0
	}
	for i := int32(0); i < 2; i++ {
		r := block.Others().At(i)
		r.ID = uint64(500 + i)
		r.X = int32(i + 3)
		r.Flag = true
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
		fmt.Fprintf(&out, "ships %%d %%d %%d %%d\n", i, r.ID, r.X, boolInt(r.Flag))
	}
	for i := int32(0); i < got.Others().Len(); i++ {
		r := got.Others().At(i)
		fmt.Fprintf(&out, "others %%d %%d %%d %%d\n", i, r.ID, r.X, boolInt(r.Flag))
	}
	if err := os.WriteFile(filepath.Join(outDir, "expected.txt"), []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCases(t *testing.T) {
	index, err := os.ReadFile(filepath.Join(outDir, "cases", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(index)), "\n")
	sort.Strings(lines)
	var out strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		name := parts[0]
		off, err := strconv.Atoi(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(outDir, "cases", name))
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, len(data)+128)
		start := (64 - int(uintptr(unsafe.Pointer(&buf[0]))%%64)) %% 64
		region := buf[start : start+len(data)]
		copy(region, data)
		var b RootBlock
		err = b.Open(unsafe.Pointer(&region[off]), int64(len(region)-off))
		verdict := "ok"
		if err != nil {
			verdict = "refused " + err.Error()
		}
		fmt.Fprintf(&out, "%%s %%s\n", name, verdict)
	}
	if err := os.WriteFile(filepath.Join(outDir, "go_reasons.txt"), []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`, dir))
}

// luauBlockDescriptor renders the IR's block descriptor graph as Luau data:
// every offset, size, kind and companion comes from the one layout model
// (docs/SPEC-TABLES.md §19.2, §8.1).
func luauBlockDescriptor(u *ir.Unit, blocks *ir.BlockUnit) string {
	bl := blocks.Block("Root")
	if bl == nil {
		panic("Root has no block form")
	}
	var b strings.Builder
	b.WriteString("--!strict\n-- written by the test from schema-go's block layout model\nreturn table.freeze({\n\troot = ")
	b.WriteString(luauBlockRecord(u, blocks, &bl.Projection, bl, true))
	b.WriteString(",\n})\n")
	return b.String()
}

func luauBlockRecord(u *ir.Unit, blocks *ir.BlockUnit, ml *ir.MemberLayout, bl *ir.BlockLayout, projection bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{name=%q, buildVersion=%s, size=%d, align=%d, fields={",
		ml.Name, luau.IntegerLit(new(big.Int).SetUint64(ir.BuildVersion(u))), ml.Size, ml.Align)
	for _, fl := range ml.Fields {
		f := fl.Field
		facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
		outOfLine := projection && ir.BlockOutOfLine(f)
		var elem string
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			elem = luauBlockRecord(u, blocks, blocks.Layout(a.ElemName), nil, false)
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			if row := blocks.Layout(ref.Name); row != nil {
				elem = luauBlockRecord(u, blocks, row, nil, false)
			}
		}
		fmt.Fprintf(&b, "\n\t{name=%q, offset=%d, size=%d, kind=%d, outOfLine=%t, isArray=%t, arrayBound=%d, elemSize=%d",
			f.Name, fl.Offset, fl.Size, ir.TableScalarKind(f), outOfLine, facts.IsArray, facts.ArrayBound, facts.ElemSize)
		if facts.CountOffset >= 0 {
			fmt.Fprintf(&b, ", countOffset=%d", facts.CountOffset)
		}
		if facts.PresentOffset >= 0 {
			fmt.Fprintf(&b, ", presentOffset=%d, optional=true", facts.PresentOffset)
		}
		if outOfLine {
			a := bl.ArrayByName(f.Name)
			fmt.Fprintf(&b, ", offsetOfOffset=%d, strideOffset=%d, stride=%d, bound=%d, elemAlign=%d",
				a.OffsetOfOffset, a.StrideOffset, a.Stride, a.Max, a.ElemAlign())
		}
		if elem != "" {
			fmt.Fprintf(&b, ", element=%s", elem)
		}
		b.WriteString("},")
	}
	b.WriteString("\n}}")
	return b.String()
}
