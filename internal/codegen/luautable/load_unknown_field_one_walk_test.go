package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
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
)

// The OLD reader's scene: a chain of node records whose last one carries a
// field the NEW writer has and this build cannot name.
const oneWalkOldSchema = `package walkold

table Link
{
    value int32 = 0
    next *Link
}

table Chain
{
    name string(8)
    link *Link
}
`

// The NEW writer adds one field to Link, so its last record names a field the
// OLD reader skips and counts unknown: ordinary version evolution, accepted by
// both readers.
const oneWalkNewSchema = `package walknew

table Link
{
    value int32 = 0
    next *Link
    extra int32 = 0
}

table Chain
{
    name string(8)
    link *Link
}
`

// oneWalkGoOracle is the generated Go of both units: the NEW builder writes a
// chain of n records whose last carries `extra`, and the OLD generated reader
// loads that wire and prints its verdict and unknown count.
const oneWalkGoOracle = `package oracle

import (
	"encoding/hex"
	"fmt"
	"testing"
	"unsafe"

	old "probe/old"
	newer "probe/new"
)

func TestOneWalkOracle(t *testing.T) {
	for _, n := range []int{10, 200} {
		var b newer.ChainBuilder
		if !b.Init() {
			t.Fatal("the new builder did not initialise")
		}
		root := b.GetRoot()
		links := make([]*newer.Link, n)
		refs := make([]newer.TableRef, n)
		for i := 0; i < n; i++ {
			p, ref := b.Main.Alloc(int64(unsafe.Sizeof(newer.Link{})))
			if p == nil {
				t.Fatal("the new builder refused a node")
			}
			links[i] = (*newer.Link)(p)
			refs[i] = ref
			newer.LinkReset(links[i])
			links[i].Value = int32(i + 1)
		}
		for i := 0; i < n-1; i++ {
			links[i].Next = refs[i+1]
		}
		links[n-1].Extra = 7
		root.Link = refs[0]
		size := newer.ChainMeasure(root, &b.Arena)
		if size < 0 {
			t.Fatal("the new builder did not measure the chain")
		}
		wire := make([]byte, size)
		if got := newer.ChainSave(root, wire, &b.Arena); got != size {
			t.Fatalf("the new builder saved %d bytes, want %d", got, size)
		}
		b.Shutdown()
		region := make([]byte, old.ChainLoadMeasure(wire))
		var report old.TableReport
		v := old.ChainLoad(region, wire, &report)
		fmt.Printf("@%d|wire:%s|accepted:%t|unknown:%d\n", n, hex.EncodeToString(wire), v != nil, report.Unknown)
	}
}
`

type oneWalkExpect struct {
	wire     []byte
	accepted bool
	unknown  int
}

// oneWalkUnit loads one schema source and writes its generated Go under dir.
func oneWalkUnit(t *testing.T, src, dir string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "walk.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the walk schema does not load: %v", err)
	}
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files["go.mod"]
}

// TestLuauLoadUnknownFieldOneWalk holds the cost of naming the unknown field:
// a strict load whose unknown counter moved answers its value, and the whole
// load takes the SAME number of whole-body walks for a 10-record chain and a
// 200-record one. The wires and the expected verdicts come from the generated
// Go of the same pair, never from a number typed here.
func TestLuauLoadUnknownFieldOneWalk(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	dir := t.TempDir()
	oneWalkUnit(t, oneWalkOldSchema, filepath.Join(dir, "old"))
	oneWalkUnit(t, oneWalkNewSchema, filepath.Join(dir, "new"))
	runtime := tfSerializeGoRuntime(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), []byte(oneWalkGoOracle), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestOneWalkOracle$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[int]oneWalkExpect{}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "@")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "|")
		if len(parts) != 4 || !strings.HasPrefix(parts[1], "wire:") || !strings.HasPrefix(parts[2], "accepted:") || !strings.HasPrefix(parts[3], "unknown:") {
			t.Fatalf("the oracle line %q is not @n|wire:...|accepted:...|unknown:...", line)
		}
		var n, unknown int
		if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
			t.Fatalf("the oracle's case %q: %v", parts[0], err)
		}
		if _, err := fmt.Sscanf(parts[3], "unknown:%d", &unknown); err != nil {
			t.Fatalf("the oracle's unknown count at %d: %v", n, err)
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(parts[1], "wire:"))
		if err != nil {
			t.Fatalf("the oracle's wire at %d: %v", n, err)
		}
		want[n] = oneWalkExpect{wire: raw, accepted: strings.TrimPrefix(parts[2], "accepted:") == "true", unknown: unknown}
	}
	if len(want) != 2 {
		t.Fatalf("the oracle printed %d cases, want 2:\n%s", len(want), out)
	}

	luDir := filepath.Join(dir, "luau")
	if err := os.MkdirAll(luDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := oneWalkLuauUnit(t)
	mods := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var probe strings.Builder
	probe.WriteString("local L = require(\"./schema_load\")\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&probe, "require(\"./%s\")\n", mod)
	}
	probe.WriteString(`local function run(key: string)
	L.resetWalkCount()
	local report: any = {}
	local v = L.load("Chain", inputs[key], report)
	print(key .. "|walks:" .. tostring(L.walkCount()) .. "|accepted:" .. tostring(v ~= nil) ..
		"|unknown:" .. tostring(report.unknown or 0))
end
run("w10")
run("w200")
`)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(luDir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"w10": want[10].wire, "w200": want[200].wire}
	out = luaurun.Run(t, luDir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		key, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("the Luau probe line %q is not key|value", line)
		}
		got[key] = rest
	}
	walks := map[int]string{}
	for _, n := range []int{10, 200} {
		key := fmt.Sprintf("w%d", n)
		w := want[n]
		wantLine := fmt.Sprintf("accepted:%t|unknown:%d", w.accepted, w.unknown)
		line := got[key]
		at := strings.Index(line, "|accepted:")
		if at < 0 || line[at+1:] != wantLine {
			t.Errorf("%s: Luau %q, want %s", key, line, wantLine)
		}
		walks[n] = line[:strings.Index(line, "|")]
	}
	if walks[10] != walks[200] {
		t.Fatalf("a strict load names the unknown field in %s for 10 records and %s for 200: the walk count grows with the record count", walks[10], walks[200])
	}
}

// oneWalkLuauUnit generates the OLD reader's Luau modules.
func oneWalkLuauUnit(t *testing.T) map[string][]byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "walk.schema")
	if err := os.WriteFile(path, []byte(oneWalkOldSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the walk schema does not load: %v", err)
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
	return files
}
