package squirreltable_test

// vt/blobs and vt/maps (docs/roadmap-evidence/squirrel.sexp): the strict read
// (ReadStrict / plain load: whole value or refusal) for blobs and maps.
// Blobs: a blob record named by two slots is blob_shared and refused whole;
// two distinct blobs round-trip. Maps: clean ascending map round-trips;
// duplicate or descending keys are malformed and refused whole.
// Negative control plants faults in copies of the real runtime artifacts
// (and in the emitted load runtime) and requires the checks to go red.
// Functional: runs the pinned Squirrel interpreter via sqrun.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// --- source gate over the real runtime artifacts ---

func vtBlobMapFindings(loadSrc, refusalSrc string) []string {
	var out []string
	checks := []struct{ src, name, want string }{
		{loadSrc, "load blob_shared graph", `if (c.fail == null) c.fail = "blob_shared";`},
		{loadSrc, "load blob_shared strict name", `report.reason <- "blob_shared";`},
		{loadSrc, "load map descending", "// DESCENDING: not a body any conforming writer produced (§2.8)"},
		{loadSrc, "load map duplicate", "// EQUAL: a REPEATED KEY is damage (docs/plans/strict-only.md decision 5)"},
		{loadSrc, "load map duplicate is damage", "report.bodyDamaged = true;"},
		{refusalSrc, "refusal malformed mapping", `bodyDamaged = "malformed"`},
	}
	for _, c := range checks {
		if !strings.Contains(c.src, c.want) {
			out = append(out, fmt.Sprintf("%s: missing %q", c.name, c.want))
		}
	}
	return out
}

func vtBlobMapSources(t *testing.T) (string, string) {
	t.Helper()
	return squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "load.nut"),
		squirrelRepoFile(t, "internal", "codegen", "squirreltable", "runtime", "refusal.nut")
}

// --- interpreter probe ---

const vtBlobMapProbe = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) { if (b == null) return "null"; local s=""; for (local i=0;i<b.len();i++) s+=HEXB[b[i]&0xff]; return s; };
local nn = function(x) { return x == null ? "-" : x.tostring(); };
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local S = schema.require("schema_table_save");
  local r = {};
  local v = R.ReadStrict(T, root, inputs[key], r);
  local saved = v == null ? null : S.save(T, root, v);
  print(key + "|" + (v == null ? "refused" : "read") + "|" + nn(("reason" in r) ? r.reason : null) + "|" + hexOf(saved) + "\n");
};
`

type vtCase struct {
	key, unit, root, wantVerdict, wantReason string
	wire                                     []byte
	wantSave                                 bool
}

func vtRunCases(t *testing.T, patchLoad func(string) string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	// blob unit from seed24 helpers
	_, mod24 := writeGeneratedUnit(t, dir, "seed24", blobNamedTwiceSeed24Unit)
	mapUnit := loadSource(t, "package probemap\n\ntable Root\n{\n    entries map[int32]int32\n}\n")
	modMap := emitUnit(t, dir, "probemap", mapUnit)
	if patchLoad != nil {
		for _, sub := range []string{"seed24", "probemap"} {
			p := filepath.Join(dir, sub, "schema_table_load.nut")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, p, []byte(patchLoad(string(b))))
		}
	}
	damaged, err := hex.DecodeString(blobNamedTwiceSeed24Wire)
	if err != nil {
		t.Fatal(err)
	}
	clean := append([]byte(nil), damaged...)
	clean[6] ^= 0x01
	cases := []vtCase{
		{"blob_clean", "seed24", "Root", "read", "-", clean, true},
		{"blob_shared", "seed24", "Root", "refused", "blob_shared", damaged, false},
		{"map_clean", "probemap", "Root", "read", "-", mapInt32Wire(t, [][2]int32{{3, 9}, {5, 1}}, nil), true},
		{"map_dup", "probemap", "Root", "refused", "malformed", mapInt32Wire(t, [][2]int32{{3, 9}, {3, 7}, {5, 1}}, nil), false},
		{"map_desc", "probemap", "Root", "refused", "malformed", mapInt32Wire(t, [][2]int32{{5, 1}, {3, 9}}, nil), false},
	}
	_ = mod24
	var p strings.Builder
	p.WriteString(vtBlobMapProbe)
	inputs := map[string][]byte{}
	mods := map[string]string{"seed24": mod24, "probemap": modMap}
	for _, c := range cases {
		inputs[c.key] = c.wire
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", c.unit, mods[c.unit], c.root, c.key)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := sqrun.Run(t, dir, "main.nut", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("bad probe line %q", line)
		}
		got[k] = v
	}
	// attach expectations for caller via got comparison done there; store wires hex in got under key+"#wire"
	for _, c := range cases {
		got[c.key+"#wire"] = hex.EncodeToString(c.wire)
	}
	return got
}

func vtExpect(t *testing.T, got map[string]string) []string {
	t.Helper()
	var fails []string
	exp := map[string][3]string{
		"blob_clean":  {"read", "-", "wire"},
		"blob_shared": {"refused", "blob_shared", ""},
		"map_clean":   {"read", "-", "wire"},
		"map_dup":     {"refused", "malformed", ""},
		"map_desc":    {"refused", "malformed", ""},
	}
	for key, e := range exp {
		line, ok := got[key]
		if !ok {
			fails = append(fails, key+": no probe line")
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			fails = append(fails, key+": bad line "+line)
			continue
		}
		wantSave := ""
		if e[2] == "wire" {
			wantSave = got[key+"#wire"]
		}
		if parts[0] != e[0] || parts[1] != e[1] || (e[2] == "wire" && parts[2] != wantSave) || (e[2] == "" && parts[2] != "null") {
			fails = append(fails, fmt.Sprintf("%s: got %s want %s|%s|%s", key, line, e[0], e[1], wantSave))
		}
	}
	return fails
}

func TestSquirrelVtBlobMapCells(t *testing.T) {
	t.Parallel()
	// source gate (no interpreter)
	loadSrc, refusalSrc := vtBlobMapSources(t)
	if f := vtBlobMapFindings(loadSrc, refusalSrc); len(f) > 0 {
		t.Fatalf("source gate: %v", f)
	}
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	got := vtRunCases(t, nil)
	if fails := vtExpect(t, got); len(fails) > 0 {
		for _, f := range fails {
			t.Error(f)
		}
	}
}

func TestSquirrelVtBlobMapCellsNegativeControl(t *testing.T) {
	t.Parallel()
	loadSrc, refusalSrc := vtBlobMapSources(t)
	if f := vtBlobMapFindings(loadSrc, refusalSrc); len(f) > 0 {
		t.Fatalf("unplanted already red: %v", f)
	}
	// plant in copies of real artifacts
	plants := []struct{ name, anchor string }{
		{"blob_shared", `if (c.fail == null) c.fail = "blob_shared";`},
		{"map duplicate", "// EQUAL: a REPEATED KEY is damage (docs/plans/strict-only.md decision 5)"},
	}
	for _, pl := range plants {
		if !strings.Contains(loadSrc, pl.anchor) {
			t.Fatalf("anchor %q not in real load.nut", pl.anchor)
		}
		planted := strings.Replace(loadSrc, pl.anchor, "// SABOTAGED", 1)
		if f := vtBlobMapFindings(planted, refusalSrc); len(f) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting %s in real load.nut left gate green", pl.name)
		}
	}
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	// plant in emitted load runtime: neutralise blob_shared graph check
	got := vtRunCases(t, func(src string) string {
		anchor := `if (blobRec && names[k + 1] > 1) {`
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("emitted load anchor count !=1")
		}
		return strings.Replace(src, anchor, `if (false && blobRec && names[k + 1] > 1) {`, 1)
	})
	fails := vtExpect(t, got)
	found := false
	for _, f := range fails {
		if strings.HasPrefix(f, "blob_shared:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("NEGATIVE CONTROL FAILED: disabling blob_shared in emitted load left blob_shared green: fails=%v got=%v", fails, got)
	}
}
