package luatable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// orphanCycleProbe runs one wire through the Lua table load paths and reports
// the verdict (accepted vs refused, never a cross-language reason string) and
// the tolerant report counters.
const orphanCycleProbe = `local T = require(inputs.module)
local load = require("load")
local refusal = require("refusal")
local retain = require("retain")
local root, wire = inputs.root, inputs.wire

local function freshReport()
  return {
    damaged = false, refused = 0, unknown = 0, kindChanged = 0,
    outOfRange = 0, widened = 0, repeatedKey = 0, sharedBlob = 0,
    missingRequired = 0, wireDamaged = false, reason = "",
    retained = 0, retainLost = 0,
  }
end

local rt = freshReport()
local vt = load.loadWalk(T, root, wire, rt)
local rl = freshReport()
local vl = load.Load(T, root, wire, rl)
local oks, vs = pcall(refusal.ReadStrict, T, root, wire)
local rr = freshReport()
local store = { list = {}, used = 0 }
local vr = retain.LoadRetain(T, root, wire, store, rr)

local accepted = (vt ~= nil and not rt.damaged and (rt.refused or 0) == 0)
local plainOK = (vl ~= nil and not rl.damaged and (rl.refused or 0) == 0)
local strictOK = (oks and vs ~= nil)
local retainOK = (vr ~= nil and not rr.damaged and (rr.refused or 0) == 0)

print(string.format("accepted=%s plain=%s strict=%s retain=%s malformed=%s u=%d w=%d",
  tostring(accepted), tostring(plainOK), tostring(strictOK), tostring(retainOK),
  tostring(rt.damaged), rt.unknown, rt.widened))
`

type orphanCycleSpec struct {
	vector, dir, root string
	from, to          string
}

var orphanCycleSpecs = []orphanCycleSpec{
	{
		vector: "pointer_orphan_cycle",
		dir:    "pointers",
		root:   "Scene",
		from:   "if not report.damaged and D.cyclic(T, desc, root, placed(ctx.nodes)) then",
		to:     "if not report.damaged and D.number_nodes(T, desc, root, false) == nil then",
	},
	{
		vector: "map_duplicate_pointer_past_table",
		dir:    "maps",
		root:   "Chunks",
		from:   "if pp.holder == out and pp.key == last then pp.holder = ctx.sink end",
		to:     "if pp.holder == out and pp.key == last then pp.holder, pp.index = ctx.sink, 0 end",
	},
}

func runOrphanCycleProbe(t *testing.T, u *ir.Unit, root string, wire []byte, patch *orphanCycleSpec) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if patch != nil {
		editLoad(t, files, patch.from, patch.to, 1)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	mainSrc := "local inputs = require(\"inputs\")\n" + orphanCycleProbe
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{
		"module": []byte(ModuleName(u)),
		"root":   []byte(root),
		"wire":   wire,
	})
	return strings.TrimSpace(string(out))
}

func goEngineVerdictAndCounts(t *testing.T, u *ir.Unit, root string, wire []byte) (accepted bool, counts string) {
	t.Helper()
	def := u.Tables[root]
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, m.New(def), wire, &rep)
	if err != nil && !tablewire.Refused(err) && !tablewire.ReadRefusal(err) {
		t.Fatalf("engine Decode(%s): %v", root, err)
	}
	var rrep tabletext.Report
	rok, rerr := tablewire.DecodeRegion(m, m.New(def), wire, &rrep)
	if rerr != nil && !tablewire.Refused(rerr) && !tablewire.ReadRefusal(rerr) {
		t.Fatalf("engine DecodeRegion(%s): %v", root, rerr)
	}
	decOK := ok && !rep.Refused && !tablewire.Refused(err)
	regOK := rok && !rrep.Refused && !tablewire.Refused(rerr)
	if decOK != regOK || rep.Refused != rrep.Refused {
		t.Fatalf("engine Decode and DecodeRegion disagree on %s: decode=(%v,%+v) region=(%v,%+v)", root, decOK, rep, regOK, rrep)
	}
	return decOK, fmt.Sprintf("malformed=%t u=%d w=%d", rep.Refused, rep.Unknown, rep.Widened)
}

func TestLuaOrphanCycleAndReplacedMapEntryVectors(t *testing.T) {
	t.Parallel()

	units := map[string]*ir.Unit{}
	wires := map[string][]byte{}
	for _, s := range orphanCycleSpecs {
		units[s.vector] = luaVectorUnit(t, s.dir)
		wires[s.vector] = luaVectorWire(t, s.vector)
	}

	// 1. Both vectors are refused by the Lua load as the Go engine refuses them
	// (verdict only, never comparing a reason name across languages).
	for _, s := range orphanCycleSpecs {
		goAccepted, _ := goEngineVerdictAndCounts(t, units[s.vector], s.root, wires[s.vector])
		if goAccepted {
			t.Fatalf("%s: the Go engine accepted a damaged vector", s.vector)
		}
		got := runOrphanCycleProbe(t, units[s.vector], s.root, wires[s.vector], nil)
		const wantRefused = "accepted=false plain=false strict=false retain=false malformed=true"
		if !strings.HasPrefix(got, wantRefused+" ") {
			t.Errorf("%s: Lua load did not refuse as the Go engine refuses: got %q, want prefix %q", s.vector, got, wantRefused)
		}
	}
	if t.Failed() {
		return
	}

	// 2. Each plant turns exactly its own vector red and leaves the other green.
	for _, plant := range orphanCycleSpecs {
		for _, vec := range orphanCycleSpecs {
			got := runOrphanCycleProbe(t, units[vec.vector], vec.root, wires[vec.vector], &plant)
			refused := strings.HasPrefix(got, "accepted=false plain=false strict=false retain=false malformed=true ")
			if plant.vector == vec.vector {
				if refused {
					t.Fatalf("plant %s left its own vector %s green: %q", plant.vector, vec.vector, got)
				}
			} else {
				if !refused {
					t.Fatalf("plant %s turned foreign vector %s red: %q", plant.vector, vec.vector, got)
				}
			}
		}
	}

	// 3. The acyclic twin of pointer_orphan_cycle (ListNode 2's next pointing to
	// ListNode 3 instead of itself) holds the same two unreached orphan records
	// and loads in both the Go engine and Lua, with the reference checks placing
	// every record (Go decodeState.cyclic, the C++ Kahn pass, and the C and Go
	// directory checks).
	orphanTwin := bytes.Clone(wires["pointer_orphan_cycle"])
	const orphanSelfEdge = "\x04\x11\x02\x00"
	const orphanAcyclicEdge = "\x04\x11\x03\x00"
	if bytes.Count(orphanTwin, []byte(orphanSelfEdge)) != 1 {
		t.Fatal("pointer_orphan_cycle must carry the self-referencing ListNode 2 body once")
	}
	orphanTwin = bytes.Replace(bytes.Replace(orphanTwin, []byte(orphanSelfEdge), []byte(orphanAcyclicEdge), 1), []byte{0x01, 0x11, 0x00}, []byte{0x01, 0x11, 0x02}, 1)
	twinGoOK, twinGoCounts := goEngineVerdictAndCounts(t, units["pointer_orphan_cycle"], "Scene", orphanTwin)
	if !twinGoOK {
		t.Fatalf("pointer_orphan_cycle acyclic twin was refused by the Go engine: %s", twinGoCounts)
	}
	twinLua := runOrphanCycleProbe(t, units["pointer_orphan_cycle"], "Scene", orphanTwin, nil)
	wantTwin := "accepted=true plain=true strict=true retain=true " + twinGoCounts
	if twinLua != wantTwin {
		t.Fatalf("pointer_orphan_cycle acyclic twin: Lua got %q, want %q", twinLua, wantTwin)
	}

	// 4. For map_duplicate_pointer_past_table, turning the replaced first entry's
	// pointer index (0x7e = 126) to an in-range node index loads and preserves
	// the reference counts on the replaced entry's slot (distinct blob 2,
	// shared blob 3, or kind-mismatched root 1), whereas the map_duplicate plant
	// drops the replaced slot's resolution counts.
	for _, tc := range []struct {
		name       string
		target     byte
		strictOK   bool
		plantLoses bool
	}{
		{"distinct_blob", 2, true, false},
		{"shared_blob", 3, false, true},
		{"root_kind_mismatch", 1, false, true},
	} {
		w := bytes.Clone(wires["map_duplicate_pointer_past_table"])
		const pastTableSlot = "\x03\x11\x7e\x00"
		if bytes.Count(w, []byte(pastTableSlot)) != 1 {
			t.Fatal("map_duplicate_pointer_past_table must carry the out-of-range pointer slot once")
		}
		w = bytes.Replace(w, []byte(pastTableSlot), []byte{0x03, 0x11, tc.target, 0x00}, 1)
		const goCounts = "malformed=false u=0 w=0"
		gotLua := runOrphanCycleProbe(t, units["map_duplicate_pointer_past_table"], "Chunks", w, nil)
		wantLua := fmt.Sprintf("accepted=true plain=%t strict=%t retain=%t %s", tc.strictOK, tc.strictOK, tc.strictOK, goCounts)
		if gotLua != wantLua {
			t.Fatalf("map_duplicate twin %s: Lua got %q, want %q", tc.name, gotLua, wantLua)
		}
		if tc.plantLoses {
			planted := runOrphanCycleProbe(t, units["map_duplicate_pointer_past_table"], "Chunks", w, &orphanCycleSpecs[1])
			if planted == wantLua {
				t.Fatalf("map_duplicate twin %s: the plant still matched the reference counts %q", tc.name, planted)
			}
		}
	}

	// 5. Verify that Go decodeState.cyclic, the C++ Kahn pass, and the C and Go
	// checks place every record in the node directory.
	repo := filepath.Join("..", "..", "..")
	for _, check := range []struct {
		path   string
		needle string
	}{
		{"internal/tablewire/decodenodes.go", "st.nodes = make([]Node, len(records))"},
		{"internal/codegen/cpptable/arena.go", "for ( int64_t p = 0; p < count; p++ )"},
		{"internal/codegen/ctable/graph.go", "for ( p = 0; p < c->count; p++ )"},
		{"internal/codegen/gotable/cycle.go", "for i:=range dir {"},
	} {
		src, err := os.ReadFile(filepath.Join(repo, check.path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(src, []byte(check.needle)) {
			t.Fatalf("%s is missing the all-records cycle vertex loop %q", check.path, check.needle)
		}
	}
}
