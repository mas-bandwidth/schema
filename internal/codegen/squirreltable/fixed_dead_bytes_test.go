package squirreltable_test

// EVERY DEAD BYTE IS ZERO on the Squirrel leg (docs/SPEC-TABLES.md §3.4, THE
// SLACK RULE; docs/FIXED-FORM-ALGORITHM.md §3.1; docs/rulings.json
// fixed-dead-bytes-zero; the owner's ruling of 2026-11-10, "Every dead byte",
// "Be strict"): an absent optional's payload, a counted array's tail past its
// live count (at the top level and inside a taken arm), a string(N) or
// bytes(N) past its used length, and a union's arm bytes under tag 0, behind a
// narrower arm and under a payload-free arm are slack, and a non-zero byte in
// any of them refuses the whole record as wire_damaged, on the identity plan
// and on an older writer's compiled plan. Each stain is a byte that would be
// lawful if it were live (a bool's 1, an ordinal in range), so the slack rule
// is the only rule that can refuse it. The negative control empties the
// runtime's slack check and requires every stain to read.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

const sqDeadOlder = "package probe\nenum Small { One, Two }\nunion Pick\n{\n    narrow uint8\n    wide uint32\n    ping\n    few [..2]uint8\n}\nfixed table D\n{\n    opt ?int32\n    arr [..3]Small\n    s string(4)\n    b bytes(4)\n    u0 Pick\n    u1 Pick\n    u2 Pick\n    u3 Pick\n    tail int32 = 7\n}\n"

// the record image, by hand from §3.4's sizes: opt 1 + 4 at 0, arr 4 + 3 at
// 5, s 4 + 4 at 12, b 4 + 4 at 20, four Picks of tag 1 + widest 6 (few: a
// count and two bytes) at 28, 35, 42 and 49, and tail at 56; 60 bytes.
var sqDeadStains = []struct {
	name string
	at   int
}{
	{"an absent optional's payload", 1},
	{"an absent optional's last payload byte", 4},
	{"a counted array's tail past its count", 10},
	{"a counted array's last tail slot", 11},
	{"a string past its used length", 18},
	{"a string's last capacity byte", 19},
	{"a byte buffer past its used length", 26},
	{"a byte buffer's last capacity byte", 27},
	{"the arm bytes under tag 0", 29},
	{"the arm bytes under tag 0, the last", 34},
	{"the arm bytes behind a narrower arm", 37},
	{"the arm bytes behind a narrower arm, the last", 41},
	{"the arm bytes under a payload-free arm", 43},
	{"the arm bytes under a payload-free arm, the last", 48},
	{"a counted array's tail inside a taken arm", 55},
}

func sqDeadBody() []byte {
	b := make([]byte, 60)
	// opt absent: 0..4 stay zero
	binary.LittleEndian.PutUint32(b[5:], 1) // arr = [One], slots 10..11 the tail
	b[9] = 1
	binary.LittleEndian.PutUint32(b[12:], 2) // s = "hi"
	b[16], b[17] = 'h', 'i'
	binary.LittleEndian.PutUint32(b[20:], 1) // b = 05
	b[24] = 5
	// u0: tag 0 at 28, arm bytes 29..34 zero
	b[35], b[36] = 1, 9 // u1: narrow = 9, bytes 37..41 zero
	b[42] = 3           // u2: ping, bytes 43..48 zero
	b[49] = 4           // u3: few = [8], the count at 50, 8 at 54, 55 the tail
	binary.LittleEndian.PutUint32(b[50:], 1)
	b[54] = 8
	binary.LittleEndian.PutUint32(b[56:], 7)
	return b
}

func TestSquirrelFixedDeadBytesRefuse(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	older := fixedSynthetic(t, sqDeadOlder)
	// the compiled plan's reader: the same table with a field appended, so the
	// older writer's record is read under ITS layout (FIXED-FORM-ALGORITHM.md
	// §5.2)
	newer := fixedSynthetic(t, strings.Replace(sqDeadOlder, "    tail int32 = 7\n", "    tail int32 = 7\n    added int32 = 5\n", 1))
	e := sqEvoEntry(t, older, "D")
	clean := sqDeadBody()
	stained := func(at int) []byte {
		b := sqEvoClone(clean)
		b[at] = 1
		return b
	}
	g := sqEvoGenerate(t, older, nil)
	// THE WRITER LEAVES ZEROS IN SLACK: the module's own save of the same
	// value is the hand-built image, byte for byte, even with a value set
	// under the absent optional.
	saved := g.save(t, "D", `local v = T.D();
v.OptPresent = false; v.Opt = 77;
v.Arr = ["One"]; v.S = "hi"; v.B = blob(1); v.B[0] = 5;
v.U0.Type = 0; v.U1.Type = 1; v.U1.Narrow = 9; v.U2.Type = 3; v.U3.Type = 4; v.U3.Few = [8];
values.append(v);
`)
	if len(saved) < len(clean) || string(saved[len(saved)-len(clean):]) != string(clean) {
		t.Fatalf("the writer's record:\n%x, want\n%x", saved, clean)
	}
	lineage := map[string][]squirreltable.FixedLineageEntry{"D": {e}}
	reads := []struct {
		name string
		g    *sqEvoGen
	}{{"identity", g}, {"compiled", sqEvoGenerate(t, newer, lineage)}}
	for _, r := range reads {
		if got := r.g.load(t, "D", sqEvoFile(e.Wire, e.Layout, clean)); got.head != "ok 1 w0" {
			t.Fatalf("%s: the clean record: %s %v", r.name, got.head, got.dump)
		}
		for _, s := range sqDeadStains {
			t.Run(r.name+"/"+s.name, func(t *testing.T) {
				t.Parallel()
				if got := sqDeadFork(t, r.g).load(t, "D", sqEvoFile(e.Wire, e.Layout, stained(s.at))); !strings.HasPrefix(got.head, refused("wire_damaged")) {
					t.Errorf("a stain in %s (body byte %d): %s, want wire_damaged", s.name, s.at, got.head)
				}
			})
		}
	}
	// THE NEGATIVE CONTROL: a runtime whose slack check passes everything
	// reads every stained record, on both plans.
	for _, r := range []struct {
		name string
		g    *sqEvoGen
	}{{"identity", sqEvoGenerate(t, older, nil)}, {"compiled", sqEvoGenerate(t, newer, lineage)}} {
		sqDeadPlantRuntime(t, r.g)
		for _, s := range sqDeadStains {
			t.Run("negative-control/"+r.name+"/"+s.name, func(t *testing.T) {
				t.Parallel()
				if got := sqDeadFork(t, r.g).load(t, "D", sqEvoFile(e.Wire, e.Layout, stained(s.at))); strings.HasPrefix(got.head, refused("wire_damaged")) {
					t.Errorf("a stain in %s still refuses with the slack check gone", s.name)
				}
			})
		}
	}
}

// TestSquirrelFixedDeadBytesRefuseOnTheWalk holds the descriptor walk's fixed
// read (schema_table_load's loadWalk, the tolerant load a fixed root takes) to
// the same slack rule: the clean record reads, every stain refuses, and the
// negative control empties the walk's slack check and requires every stain to
// read.
func TestSquirrelFixedDeadBytesRefuseOnTheWalk(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	older := fixedSynthetic(t, sqDeadOlder)
	e := sqEvoEntry(t, older, "D")
	clean := sqDeadBody()
	walk := func(t *testing.T, g *sqEvoGen, body []byte) string {
		t.Helper()
		out, err := g.exec(t, fmt.Sprintf(`local S = dofile("schema_module.nut", true);
local T = S.require(%q);
local L = S.require("schema_table_load");
local inputs = dofile("inputs.nut", true);
local report = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false };
local v = L.loadWalk(T, "D", inputs["file"], report);
print((v == null ? "null" : "value") + " refused=" + report.refused + "\n");
`, g.mod), map[string][]byte{"file": sqEvoFile(e.Wire, e.Layout, body)})
		if err != nil {
			t.Fatalf("sq: %v\n%s", err, out)
		}
		return strings.TrimSpace(out)
	}
	stained := func(at int) []byte {
		b := sqEvoClone(clean)
		b[at] = 1
		return b
	}
	g := sqEvoGenerate(t, older, nil)
	if got := walk(t, g, clean); got != "value refused=false" {
		t.Fatalf("the clean record on the walk: %s", got)
	}
	for _, s := range sqDeadStains {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			if got := walk(t, sqDeadFork(t, g), stained(s.at)); got != "null refused=true" {
				t.Errorf("a stain in %s (body byte %d) on the walk: %s, want refused", s.name, s.at, got)
			}
		})
	}
	// THE NEGATIVE CONTROL: the walk with its slack check gone reads every stain
	gc := sqEvoGenerate(t, older, nil)
	const anchor = "  for (local i = 0; i < n; i++) if ((bytes[off + i] & 0xff) != 0) return \"wire_damaged\";\n"
	hit := 0
	for name, data := range gc.files {
		if strings.Count(string(data), anchor) == 1 {
			gc.files[name] = []byte(strings.Replace(string(data), anchor, "", 1))
			sqWriteFiles(t, gc.dir, map[string][]byte{name: gc.files[name]})
			hit++
		}
	}
	if hit != 1 {
		t.Fatalf("the walk's slack anchor is in %d generated files, want 1", hit)
	}
	for _, s := range sqDeadStains {
		t.Run("negative-control/"+s.name, func(t *testing.T) {
			t.Parallel()
			if got := walk(t, sqDeadFork(t, gc), stained(s.at)); got != "value refused=false" {
				t.Errorf("a stain in %s still refuses on the walk with the slack check gone: %s", s.name, got)
			}
		})
	}
}

// sqDeadPlantRuntime empties the runtime's deadZero in g's copy.
func sqDeadPlantRuntime(t *testing.T, g *sqEvoGen) {
	t.Helper()
	const anchor = "  for (local i = 0; i < n; i++) { if (b[at + i] != 0) return false; }\n"
	hit := 0
	for name, data := range g.files {
		if strings.Count(string(data), anchor) == 1 {
			g.files[name] = []byte(strings.Replace(string(data), anchor, "", 1))
			sqWriteFiles(t, g.dir, map[string][]byte{name: g.files[name]})
			hit++
		}
	}
	if hit != 1 {
		t.Fatalf("the deadZero anchor is in %d generated files, want 1", hit)
	}
}

// sqDeadFork copies g's files into a directory of the subtest's own, so
// parallel loads never share a probe file.
func sqDeadFork(t *testing.T, g *sqEvoGen) *sqEvoGen {
	t.Helper()
	f := &sqEvoGen{unit: g.unit, dir: t.TempDir(), mod: g.mod, files: maps.Clone(g.files)}
	sqWriteFiles(t, f.dir, f.files)
	return f
}
