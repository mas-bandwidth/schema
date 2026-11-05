// The Squirrel target carries the fixed-point and 128-bit table-wire kinds on
// the variable wire and JSON (docs/SPEC-TABLES.md §3, §15, §16.2).
// ir.WideTableKindScope (ir/tablekind.go) scopes §15's refusal to the form-1
// accelerators (block and cook), so a leg with no block and no cook writer
// carries the wide kinds without refusing the unit.
package compiler

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const squirrelWideKindsPrelude = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) {
  if (b == null) return "null";
  if (b.len() == 0) return "-";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += HEXB[b[i] & 0xff];
  return s;
};
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
};
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local J = schema.require("schema_table_json");
  local v = L.loadTolerant(T, root, inputs[key], fresh());
  if (v == null) { print("FAIL " + key + " load\n"); return; }
  local saved = S.save(T, root, v);
  if (saved == null) { print("FAIL " + key + " save\n"); return; }
  local text = J.ToJson(T, root, v);
  if (text == null) { print("FAIL " + key + " json\n"); return; }
  print("OK " + key + " " + hexOf(saved) + " " + hexOf(text) + "\n");
};
`

type wideManifestInstance struct {
	name string
	unit string
	root string
	wire string
}

// TestSquirrelCarriesWideTableKinds proves the scalars and tblscalars2 units
// generate for squirrel (docs/SPEC-TABLES.md §3, §15, §16.2) and that for
// every instance row of testdata/conformance/tables/MANIFEST.txt on those
// units, the Squirrel save of the loaded value equals the instance wire byte
// for byte and the JSON text equals internal/tabletext's (the Go oracle) for
// the same value.
func TestSquirrelCarriesWideTableKinds(t *testing.T) {
	t.Parallel()
	repoRoot, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(repoRoot, "testdata", "conformance", "tables", "MANIFEST.txt")
	mf, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()

	unitRel := map[string][]string{}
	var instances []wideManifestInstance
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 3 && fields[0] == "unit" {
			if fields[1] == "scalars" || fields[1] == "tblscalars2" {
				unitRel[fields[1]] = fields[2:]
			}
		}
		if len(fields) >= 5 && fields[0] == "instance" {
			if fields[2] == "scalars" || fields[2] == "tblscalars2" {
				instances = append(instances, wideManifestInstance{
					name: fields[1],
					unit: fields[2],
					root: fields[3],
					wire: fields[4],
				})
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	c := New()
	dir := t.TempDir()
	units := map[string]*ir.Unit{}
	mods := map[string]string{}
	for _, key := range []string{"scalars", "tblscalars2"} {
		rel, ok := unitRel[key]
		if !ok {
			t.Fatalf("manifest declares no unit %s", key)
		}
		args := make([]string, len(rel))
		for i, r := range rel {
			args[i] = filepath.Join(repoRoot, filepath.FromSlash(r))
		}
		paths, err := GatherPaths(args)
		if err != nil {
			t.Fatal(err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("load %s: %v", key, err)
		}
		files, err := c.Generate(u, "squirrel", Options{})
		if err != nil {
			t.Fatalf("squirrel refused %s: %v", key, err)
		}
		if len(files) == 0 {
			t.Fatalf("squirrel emitted no files for %s", key)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		units[key] = u
		mods[key] = squirreltable.ModuleName(u)
	}

	if len(instances) == 0 {
		t.Fatal("manifest declares no instance rows on scalars or tblscalars2")
	}

	var probe strings.Builder
	probe.WriteString(squirrelWideKindsPrelude)
	inputs := map[string][]byte{}
	wantWire := map[string][]byte{}
	wantJSON := map[string][]byte{}
	for _, in := range instances {
		wireBytes, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = wireBytes
		wantWire[in.name] = wireBytes

		u := units[in.unit]
		model := tabletext.NewModel(u)
		st := model.Lookup(in.root)
		if st == nil {
			t.Fatalf("%s: unit %s declares no root %s", in.name, in.unit, in.root)
		}
		val := model.New(st)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, val, wireBytes, &rep); err != nil || !ok || rep.Malformed || rep.Refused {
			t.Fatalf("%s: tablewire.Decode failed (ok=%v err=%v report=%+v)", in.name, ok, err, rep)
		}
		jsonBytes, err := model.Write(val)
		if err != nil {
			t.Fatalf("%s: tabletext.Write: %v", in.name, err)
		}
		if !bytes.HasSuffix(jsonBytes, []byte("\n")) {
			jsonBytes = append(jsonBytes, '\n')
		}
		wantJSON[in.name] = jsonBytes
		fmt.Fprintf(&probe, "run(%q, %q, %q, %q);\n", in.unit, mods[in.unit], in.root, in.name)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	gotWire := map[string][]byte{}
	gotJSON := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != "OK" {
			t.Fatalf("unexpected probe line %q", line)
		}
		wb, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("%s wire hex: %v", fields[1], err)
		}
		jb, err := hex.DecodeString(fields[3])
		if err != nil {
			t.Fatalf("%s json hex: %v", fields[1], err)
		}
		gotWire[fields[1]] = wb
		gotJSON[fields[1]] = jb
	}

	compared := 0
	for _, in := range instances {
		wb, ok := gotWire[in.name]
		if !ok {
			t.Fatalf("%s: probe emitted no result", in.name)
		}
		if !bytes.Equal(wb, wantWire[in.name]) {
			t.Errorf("%s: Squirrel save differs from instance wire\n got  %x\n want %x", in.name, wb, wantWire[in.name])
		}
		jb := gotJSON[in.name]
		if !bytes.Equal(jb, wantJSON[in.name]) {
			t.Errorf("%s: Squirrel ToJson differs from tabletext oracle\n got  %q\n want %q", in.name, jb, wantJSON[in.name])
		}
		compared++
	}
	t.Logf("compared %d instance rows across scalars and tblscalars2", compared)
}
