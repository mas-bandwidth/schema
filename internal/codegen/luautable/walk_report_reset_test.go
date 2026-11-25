package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauWalkReadsZeroAReusedReport holds the walk reads to docs/SPEC-TABLES.md
// §6.5: EVERY READ ZEROES A SUPPLIED REPORT IN FULL as its first act. The walk
// refuses on the counters the report holds (walkRefuses, judgeWalk), so a
// report left dirty by an earlier refused read must not refuse a whole wire
// read into it next. One report is reused: a damaged wire both the oracle and
// the leg refuse, then a whole wire the oracle reads, which L.loadWalk and
// R.loadRetainWalk must read through with a clean report.
func TestLuauWalkReadsZeroAReusedReport(t *testing.T) {
	t.Parallel()
	dir, mods, _ := luauDamageUnit(t)

	// the oracle's own wire for the root the leg reads (Cfg as a variable root,
	// as luauDamageUnit emits it), and its verdicts on both wires
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["tblr1"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	u.Tables["Cfg"].FixedDeclared = false
	m := tabletext.NewModel(u)
	clean, err := tablewire.Encode(m, m.New(m.Lookup("Cfg")))
	if err != nil {
		t.Fatal(err)
	}
	var cr tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Cfg")), clean, &cr); !ok || err != nil || cr.Refused {
		t.Fatalf("the oracle does not read its own whole wire: %v %v %+v", ok, err, cr)
	}
	damaged, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/frame_slack_after_terminator.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var dr tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Cfg")), damaged, &dr); ok && err == nil && !dr.Refused {
		t.Fatalf("the oracle reads the damaged wire, so it cannot dirty the report: %+v", dr)
	}

	var b strings.Builder
	b.WriteString("local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&b, "require(%q)\n", "./"+mod)
	}
	b.WriteString(`local function read(retain: boolean, wire: buffer, report: any)
 if retain then return R.loadRetainWalk("Cfg", wire, report) end
 return L.loadWalk("Cfg", wire, report)
end
for _, retain in {false, true} do
 local report = {malformed=false, refused=0, unknown=0, widened=0}
 local first = read(retain, inputs.damaged, report)
 local second = read(retain, inputs.clean, report)
 print(tostring(first == nil) .. " " .. tostring(second ~= nil) .. " " .. tostring(report.malformed) .. " " .. tostring(report.reason))
end
`)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"damaged": damaged, "clean": clean})))
	// per line: the damaged read refused, the whole read answered a value, and
	// the reused report holds none of the refused read's damage
	const want = "true true false nil\ntrue true false nil"
	if got != want {
		t.Fatalf("a report reused after a refused read refused a whole wire (loadWalk line, then loadRetainWalk):\ngot  %q\nwant %q", got, want)
	}
}
