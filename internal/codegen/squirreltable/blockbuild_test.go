package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// blockBuilderProbe builds both block corpus instances through the generated
// Squirrel builder (docs/SPEC-TABLES.md §19.1) and prints their used extent as
// one lowercase hex line each. The values are the C++ producer's own
// (test/tables/block_main.cpp), so a byte match is a comparison over values
// rather than over a blob both sides copied.
const blockBuilderProbe = `local schema = dofile("blockdemo/schema_module.nut", true);
local T = schema.require("BlockdemoTables");
local fresh = function() { return { Array = null, Count = 0, Maximum = 0 }; };
local countsOf = function() {
  return { cameras = 1, ships = 5, turrets = 7, missiles = 3, dynamic_props = 2,
           static_props = 4, cosmetic_props = 3, lasers = 6, explosions = 2 };
};
local setf = function(block, record, at, path, value) {
  if (!T.blockSet(block, record, at, path, value)) print("FAIL set " + record + "." + path + "\n");
};
local vec3 = function(block, record, at, prefix, salt, i) {
  setf(block, record, at, prefix + ".x", 1.5 * i + salt);
  setf(block, record, at, prefix + ".y", 2.25 * i - salt);
  setf(block, record, at, prefix + ".z", 0.5 * i + 2 * salt);
};
local quat = function(block, record, at, prefix, salt, i) {
  setf(block, record, at, prefix + ".x", 0.25 * i + salt);
  setf(block, record, at, prefix + ".y", 0.125 * i);
  setf(block, record, at, prefix + ".z", 0.0625 * i - salt);
  setf(block, record, at, prefix + ".w", 1.0 - 0.5 * (i % 3));
};
local fillCamera = function(block, at, i) {
  vec3(block, "RenderCamera", at, "position", 1, i);
  quat(block, "RenderCamera", at, "rotation", 1, i);
  setf(block, "RenderCamera", at, "camera_id", i * 7 + 1);
  setf(block, "RenderCamera", at, "camera_type", i % 4);
  setf(block, "RenderCamera", at, "target_object_id", i * 13 + 2);
  setf(block, "RenderCamera", at, "fov", 0.5 * i + 60.0);
};
local fillShip = function(block, at, i) {
  vec3(block, "RenderShip", at, "position", 2, i);
  quat(block, "RenderShip", at, "rotation", 2, i);
  setf(block, "RenderShip", at, "flags", i % 16);
  setf(block, "RenderShip", at, "object_id", i * 3 + 11);
  setf(block, "RenderShip", at, "target_object_id", i * 5 + 7);
  setf(block, "RenderShip", at, "thrust", 0.25 * i);
  setf(block, "RenderShip", at, "object_sequence", i % 251);
  setf(block, "RenderShip", at, "ship_type", i % 4);
  setf(block, "RenderShip", at, "team", i % 5);
  setf(block, "RenderShip", at, "has_target_lock", (i % 2) == 0);
  setf(block, "RenderShip", at, "predicted_explode", (i % 3) == 0);
};
local fillTurret = function(block, at, i) {
  quat(block, "RenderTurret", at, "rotation", 3, i);
  setf(block, "RenderTurret", at, "flags", i * 17);
  setf(block, "RenderTurret", at, "object_id", i * 2 + 1);
  setf(block, "RenderTurret", at, "parent_object_id", i / 3);
  setf(block, "RenderTurret", at, "turret_index", i % 8);
  setf(block, "RenderTurret", at, "target_object_id", i * 11);
  setf(block, "RenderTurret", at, "object_sequence", i % 253);
  setf(block, "RenderTurret", at, "team", i % 5);
  setf(block, "RenderTurret", at, "has_target_lock", (i % 5) == 0);
};
local fillMissile = function(block, at, i) {
  vec3(block, "RenderMissile", at, "position", 4, i);
  quat(block, "RenderMissile", at, "rotation", 4, i);
  setf(block, "RenderMissile", at, "flags", i * 19);
  setf(block, "RenderMissile", at, "object_id", i * 23);
  setf(block, "RenderMissile", at, "object_sequence", i % 249);
  setf(block, "RenderMissile", at, "missile_type", i % 3);
  setf(block, "RenderMissile", at, "team", i % 5);
};
local fillDynamic = function(block, at, i) {
  vec3(block, "RenderDynamicProp", at, "position", 5, i);
  quat(block, "RenderDynamicProp", at, "rotation", 5, i);
  setf(block, "RenderDynamicProp", at, "flags", i * 29);
  setf(block, "RenderDynamicProp", at, "object_id", i * 31);
  setf(block, "RenderDynamicProp", at, "object_sequence", i % 247);
  setf(block, "RenderDynamicProp", at, "prop_type", i % 4);
  setf(block, "RenderDynamicProp", at, "team", i % 5);
};
local fillStatic = function(block, at, i) {
  vec3(block, "RenderStaticProp", at, "position", 6, i);
  quat(block, "RenderStaticProp", at, "rotation", 6, i);
  setf(block, "RenderStaticProp", at, "scale", 0.5 + 0.25 * (i % 7));
  setf(block, "RenderStaticProp", at, "flags", i * 37);
  setf(block, "RenderStaticProp", at, "static_prop_id", i * 41);
  setf(block, "RenderStaticProp", at, "prop_type", i % 4);
  setf(block, "RenderStaticProp", at, "team", i % 5);
};
local fillCosmetic = function(block, at, i) {
  vec3(block, "RenderCosmeticProp", at, "position", 7, i);
  quat(block, "RenderCosmeticProp", at, "rotation", 7, i);
  setf(block, "RenderCosmeticProp", at, "scale", 0.25 + 0.125 * (i % 5));
  setf(block, "RenderCosmeticProp", at, "flags", i * 43);
  setf(block, "RenderCosmeticProp", at, "cosmetic_prop_id", i * 47);
  setf(block, "RenderCosmeticProp", at, "prop_sequence", i % 241);
  setf(block, "RenderCosmeticProp", at, "prop_type", i % 4);
  setf(block, "RenderCosmeticProp", at, "team", i % 5);
};
local fillLaser = function(block, at, i) {
  vec3(block, "RenderLaser", at, "start", 8, i);
  vec3(block, "RenderLaser", at, "finish", 9, i);
  setf(block, "RenderLaser", at, "t", 0.125 * (i % 8));
  setf(block, "RenderLaser", at, "laser_id", i * 53);
  setf(block, "RenderLaser", at, "laser_type", i % 3);
  setf(block, "RenderLaser", at, "team", i % 5);
};
local fillExplosion = function(block, at, i) {
  vec3(block, "RenderExplosion", at, "position", 10, i);
  quat(block, "RenderExplosion", at, "rotation", 10, i);
  setf(block, "RenderExplosion", at, "t", 0.0625 * (i % 16));
  setf(block, "RenderExplosion", at, "explosion_id", i * 59);
  setf(block, "RenderExplosion", at, "parent_object_id", i * 61);
  setf(block, "RenderExplosion", at, "explosion_type", i % 3);
  setf(block, "RenderExplosion", at, "team", i % 5);
};
local fillRender = function(block) {
  setf(block, "RenderFrame", 0, "version", 1);
  local a = T.blockRows(block, "cameras");
  for (local i = 0; i < a.Count; i++) fillCamera(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "ships");
  for (local i = 0; i < a.Count; i++) fillShip(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "turrets");
  for (local i = 0; i < a.Count; i++) fillTurret(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "missiles");
  for (local i = 0; i < a.Count; i++) fillMissile(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "dynamic_props");
  for (local i = 0; i < a.Count; i++) fillDynamic(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "static_props");
  for (local i = 0; i < a.Count; i++) fillStatic(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "cosmetic_props");
  for (local i = 0; i < a.Count; i++) fillCosmetic(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "lasers");
  for (local i = 0; i < a.Count; i++) fillLaser(block, a.At + i * a.Stride, i);
  a = T.blockRows(block, "explosions");
  for (local i = 0; i < a.Count; i++) fillExplosion(block, a.At + i * a.Stride, i);
};
local fillPadded = function(block) {
  setf(block, "PaddedFrame", 0, "marker", 7);
  setf(block, "PaddedFrame", 0, "stamp", 0x0123456789abcdef);
  setf(block, "PaddedFrame", 0, "blob", "twelve bytes");
  local a = T.blockRows(block, "rows");
  for (local i = 0; i < a.Count; i++) {
    local at = a.At + i * a.Stride;
    setf(block, "PaddedRow", at, "tag", 10 + i);
    setf(block, "PaddedRow", at, "value", 0.5 * i + 100.0);
    setf(block, "PaddedRow", at, "flag", (i % 2) == 0);
    setf(block, "PaddedRow", at, "id", i * 1000 + 3);
    setf(block, "PaddedRow", at, "label", format("row-%d", i));
    for (local s = 0; s < 4; s++) setf(block, "PaddedRow", at, "slots[" + s + "]", i * 4 + s);
    for (local t = 1; t <= 4; t++) setf(block, "PaddedRow", at, "teams[" + t + "]", i + t);
    setf(block, "PaddedRow", at, "counter", i * 9);
    setf(block, "PaddedRow", at, "counter:present", (i % 2) == 1);
  }
};
local build = function(root, counts, fill, over, negative) {
  local allocs = 0, frees = 0;
  local alloc = function(n) {
    allocs++;
    local b = blob(n);
    for (local i = 0; i < n; i++) b[i] = 0;
    return b;
  };
  local free = function(b) { frees++; };
  local storage = T.blockCreate(root, alloc, free);
  if (storage == null) { print("FAIL create " + root + "\n"); return; }
  local refusal = fresh();
  local block = T.blockBegin(root, storage, counts, refusal);
  if (block == null) { print("FAIL begin " + root + "\n"); return; }
  fill(block);
  local used = T.blockBytes(block);
  local b = block.Blob, origin = block.Base;
  local s = "";
  for (local i = 0; i < used; i++) s += format("%02x", b[origin + i] & 0xff);
  print("BLOCK " + root + " " + used + " " + s + "\n");
  local bad = fresh();
  if (T.blockBegin(root, storage, over, bad) != null) print("FAIL over " + root + "\n");
  if (bad.Array != over.__array || bad.Count != over.__count || bad.Maximum != over.__max) {
    print("FAIL refusal " + root + " " + (bad.Array == null ? "?" : bad.Array) + "\n");
  }
  bad = fresh();
  if (T.blockBegin(root, storage, negative, bad) != null) print("FAIL negative " + root + "\n");
  if (bad.Array != negative.__array || bad.Count != negative.__count) {
    print("FAIL negative refusal " + root + "\n");
  }
  // the refused calls changed no byte: the good block still measures the same
  if (T.blockBytes(block) != used) print("FAIL refusal changed " + root + "\n");
  if (allocs != 1) print("FAIL allocs " + root + " " + allocs + "\n");
  T.blockDestroy(storage);
  if (frees != 1) print("FAIL frees " + root + " " + frees + "\n");
  if (T.blockBegin(root, storage, counts, null) != null) print("FAIL destroyed " + root + "\n");
};
local overRender = countsOf();
overRender.ships = 4097;
overRender.__array <- "ships"; overRender.__count <- 4097; overRender.__max <- 4096;
local negRender = countsOf();
negRender.lasers = -1;
negRender.__array <- "lasers"; negRender.__count <- -1;
build("RenderFrame", countsOf(), fillRender, overRender, negRender);
local padded = { rows = 4 };
local overPadded = { rows = 65, __array = "rows", __count = 65, __max = 64 };
local negPadded = { rows = -1, __array = "rows", __count = -1 };
build("PaddedFrame", padded, fillPadded, overPadded, negPadded);
print("count 2\n");
`

// TestSquirrelBlockBuilderMatchesTheCorpus builds every block instance the
// corpus pins through the generated Squirrel builder (docs/SPEC-TABLES.md
// §19.1) and compares its used extent, byte for byte, with the pinned block.
// It then replays Go's refusals: a count past its declared maximum and a
// negative count are refused, naming the array, the count and the maximum,
// the refused call leaves the built block untouched, and the caller's
// allocator is asked exactly once for the storage lifetime.
func TestSquirrelBlockBuilderMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/block"})
	dir := t.TempDir()
	out := filepath.Join(dir, "blockdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(blockBuilderProbe))

	stdout := sqrun.Run(t, dir, "main.nut", nil)
	pinned := map[string]string{
		"RenderFrame": "block_render",
		"PaddedFrame": "block_padded",
	}
	got := map[string]string{}
	count := 0
	claimed := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 4 && fields[0] == "BLOCK":
			got[fields[1]] = fields[3]
			if fields[2] != fmt.Sprint(len(fields[3])/2) {
				t.Errorf("%s: the builder measured %s bytes, the hex line carries %d", fields[1], fields[2], len(fields[3])/2)
			}
		case len(fields) == 2 && fields[0] == "count":
			fmt.Sscan(fields[1], &claimed)
		case len(fields) > 0 && fields[0] == "FAIL":
			t.Errorf("the Squirrel builder probe reported: %s", line)
		case line != "":
			t.Errorf("unexpected output %q", line)
		}
	}
	for table, name := range pinned {
		want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if got[table] != fmt.Sprintf("%x", want) {
			first := -1
			shown := min(len(got[table])/2, len(want))
			for i := range shown {
				if got[table][2*i:2*i+2] != fmt.Sprintf("%02x", want[i]) {
					first = i
					break
				}
			}
			if first < 0 && len(got[table])/2 != len(want) {
				first = shown
			}
			t.Errorf("%s: the Squirrel builder's bytes do not match testdata/wire/tables/%s.bin (%d bytes); first difference at %d", table, name, len(want), first)
			continue
		}
		count++
	}
	if count != len(pinned) || claimed != len(pinned) {
		t.Errorf("built %d of %d block corpus instances (probe counted %d)", count, len(pinned), claimed)
	}
	t.Logf("built %d block corpus instances", count)
}
