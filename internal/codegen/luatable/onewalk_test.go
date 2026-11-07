package luatable

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ONE WALK FOR THE NUMBERING, THE CARD'S SIZING AND ITS PACK (docs/PORTING.md
// M15). The pointer-graph walk is descriptor.lua's D.number_nodes and nothing
// else: the save, the retaining save, the cook's layout (whose sizing and pack
// share its extent walk, carve_extent, with the writer nil or set) and the
// JSON text form's reference count each CALL it. Two halves hold that. The
// structural half reads the runtime: the edge walk is defined once, and each
// consumer names it. The behavioural half is the corpus row the method names,
// stream_arm_first, whose numbering differs between a declaration-order walk
// and the grouped one (every pointer field, then every by-value nesting, then
// every union arm): its wire, rebuilt by Save, and its cook, rebuilt by Cook,
// must be the bytes the engine wrote.

var edgeWalkDef = regexp.MustCompile(`(?m)^(?:local function edge_value|edge_value = function)`)

func TestLuaOnePointerWalk(t *testing.T) {
	t.Parallel()
	names, err := fs.Glob(runtime, "runtime/*.lua")
	if err != nil {
		t.Fatal(err)
	}
	defs := 0
	for _, n := range names {
		src, err := fs.ReadFile(runtime, n)
		if err != nil {
			t.Fatal(err)
		}
		base := strings.TrimPrefix(n, "runtime/")
		got := len(edgeWalkDef.FindAll(src, -1))
		defs += got
		if got > 0 && base != "descriptor.lua" {
			t.Errorf("%s carries its own copy of the edge walk (%d definition(s)): the one walk is descriptor.lua's D.number_nodes (docs/PORTING.md M15)", base, got)
		}
		switch base {
		case "save.lua", "retain.lua", "cookwrite.lua", "json.lua":
			if !strings.Contains(string(src), "D.number_nodes(") {
				t.Errorf("%s numbers nodes without D.number_nodes", base)
			}
		}
	}
	if defs != 1 {
		t.Errorf("the edge walk is defined %d times in runtime/*.lua, want exactly 1", defs)
	}
}

// walkRows are the corpus rows that reach a by-value edge before a pointer:
// the unit, the root and the instance case.
var walkRows = []struct{ unit, root, name string }{
	{"streamdemo", "Feed", "stream_arm_first"},
	{"listdemo", "Album", "list_before_pointer"},
	{"tblg1", "Guarded", "optional_absent_no_edge"},
}

// walkProbe loads the wire, saves it back and cooks it, and says whether each
// is the bytes the engine wrote (the cook is compared where the corpus has one).
func walkProbe(module, root string) string {
	return fmt.Sprintf(`local T = require(%q)
local load, save, cookwrite = require("load"), require("save"), require("cookwrite")
local inputs = require("inputs")
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local v = load.LoadTolerant(T, %q, inputs.wire, report())
print("wire", save.Save(T, %q, v) == inputs.wire)
if inputs.cook ~= nil then print("cook", cookwrite.Cook(T, %q, v) == inputs.cook) end
`, module, root, root, root)
}

// walkAnswers runs one row through the (possibly sabotaged) unit's files and
// answers its rows by name.
func walkAnswers(t *testing.T, module string, files map[string][]byte, root, name string) map[string]string {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"wire": wire}
	if cook, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", name+".cook")); err == nil {
		inputs["cook"] = cook
	}
	out, stderr, runErr := try(t, files, walkProbe(module, root), inputs)
	if runErr != nil {
		t.Fatalf("%s: %v\n%s", name, runErr, stderr)
	}
	rows := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, _ := strings.Cut(line, "\t")
		rows[k] = v
	}
	return rows
}

// groupedWalk is the shape the method refuses: every pointer and blob field
// first, then everything else, in the same walk.
const (
	walkLoop = `  for _, fd in ipairs(desc.fields) do
    if D.guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
      edge_value(T, fd, v[fd.name], v, fd.name, out, cache)
    end
  end
  return out
`
	groupedLoop = `  for pass = 1, 2 do
    for _, fd in ipairs(desc.fields) do
      local e = fd.shape == "array" and fd.elem or fd
      local direct = e.shape == "pointer" or e.shape == "string_blob" or e.shape == "bytes_blob"
      if (pass == 1) == direct and D.guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
        edge_value(T, fd, v[fd.name], v, fd.name, out, cache)
      end
    end
  end
  return out
`
)

func TestLuaOnePointerWalkRowsAgreeWithTheEngine(t *testing.T) {
	t.Parallel()
	for _, r := range walkRows {
		b := buildManifestUnit(t, r.unit)
		got := walkAnswers(t, b.module, b.files, r.root, r.name)
		for k, v := range got {
			if v != "true" {
				t.Errorf("%s: the %s is not the engine's bytes under the one walk", r.name, k)
			}
		}
		if got["wire"] != "true" {
			t.Errorf("%s: no wire answer", r.name)
		}
	}
}

// TestLuaOnePointerWalkNegativeControl puts the grouped walk into the one
// walk: stream_arm_first, whose by-value edge is declared before a pointer
// field, must turn red on exactly the two surfaces a numbering reaches here
// (the saved wire and the cook), and the rows with no such edge stay green.
func TestLuaOnePointerWalkNegativeControl(t *testing.T) {
	t.Parallel()
	for _, r := range walkRows {
		b := buildManifestUnit(t, r.unit)
		files := b.patched(t, "descriptor.lua", walkLoop, groupedLoop)
		got := walkAnswers(t, b.module, files, r.root, r.name)
		red := got["wire"] != "true" || (got["cook"] != "" && got["cook"] != "true")
		if r.name == "stream_arm_first" {
			if got["wire"] != "false" || got["cook"] != "false" {
				t.Errorf("stream_arm_first stayed green under the grouped walk: %v", got)
			}
		} else if red {
			t.Logf("%s also differs under the grouped walk: %v", r.name, got)
		}
	}
}
