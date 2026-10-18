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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refusalsPrelude is the strict-read probe: every case loads its unit through
// the loader, optionally forges a form byte, optionally declares integrity on
// the root descriptor, then calls ReadStrict and prints its named reason
// (docs/SPEC-TABLES.md §6.5, §11, §2.14).
const refusalsPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key, form, integrity) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local b = inputs[key];
  if (form != null) { b = blob(1); b.writen(form, 'b'); }
  if (integrity) { T.Descriptors[root].integrity <- "crc32c"; }
  local report = {};
  local v = R.ReadStrict(T, root, b, report);
  print(key + "|" + (report.reason == null ? "ok" : report.reason) + "\n");
};
`

type refusalCase struct {
	key       string
	unit      string
	root      string
	input     string
	form      int // -1 for the input's own bytes
	integrity bool
	want      string
}

// TestSquirrelStrictReadRefusesByName drives ReadStrict over one wire per
// named refusal (docs/SPEC-TABLES.md §6.5, §11): the manifest's report rows
// where a counter moved, a constructed duplicate field, and a forged form byte
// per direction. Each must be refused under its own name.
func TestSquirrelStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	rows := readReportRows(t, manifest)
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	rowByName := map[string]reportRow{}
	for _, r := range rows {
		rowByName[r.name] = r
	}

	// one manifest row per counter that moved alone, over a VARIABLE root (a
	// fixed root refuses form 1 as previous_form instead of reading it)
	manifestCases := []struct {
		key, row, want string
		field          int
	}{
		{"clamped", "parts_count_past_bound", "clamped", 3},
		{"kind_mismatch", "parts_elem_kind", "kind_mismatch", 1},
		{"malformed", "parts_index_out_of_range", "malformed", 5},
	}
	var cases []refusalCase
	for _, c := range manifestCases {
		row, ok := rowByName[c.row]
		if !ok {
			t.Fatalf("the manifest names no report row %q", c.row)
		}
		counts := strings.Split(pinned[c.row], ",")
		if counts[c.field] == "0" {
			t.Fatalf("%s: the pinned report %q does not move that counter", c.row, pinned[c.row])
		}
		cases = append(cases, refusalCase{key: c.key, unit: row.unit, root: row.root, input: c.row, form: -1, want: c.want})
	}
	cases = append(cases,
		refusalCase{key: "duplicate", unit: "streamdemo", root: "Feed", input: "dup", form: -1, want: "duplicate"},
		refusalCase{key: "unknown", unit: "streamdemo", root: "Feed", input: "unknown", form: -1, want: "unknown"},
		refusalCase{key: "previous_form", unit: "tblv1", root: "Cfg", input: "form_previous", form: 1, want: "previous_form"},
		refusalCase{key: "message_form_as_file", unit: "streamdemo", root: "Feed", input: "form_message", form: 2, want: "message_form_as_file"},
		refusalCase{key: "newer_form", unit: "streamdemo", root: "Feed", input: "form_newer", form: 3, want: "newer_form"},
		refusalCase{key: "unknown_form", unit: "streamdemo", root: "Feed", input: "form_unknown", form: 127, want: "newer_form"},
		refusalCase{key: "integrity_mismatch", unit: "streamdemo", root: "Feed", input: "integrity", form: -1, integrity: true, want: "integrity_mismatch"},
	)

	need := map[string]bool{}
	for _, c := range cases {
		need[c.unit] = true
	}
	dir := t.TempDir()
	inputs := map[string][]byte{}
	mods := map[string]string{}
	for _, u := range units {
		if !need[u.key] {
			continue
		}
		unit := loadUnit(t, root, u.paths)
		files, err := squirrel.Generate(unit)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", u.key, err)
		}
		tables, err := squirreltable.Generate(unit)
		if err != nil {
			t.Fatalf("unit %s: the table surface refused: %v", u.key, err)
		}
		out := filepath.Join(dir, u.key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mods[u.key] = squirreltable.ModuleName(unit)
		for _, r := range rows {
			if r.unit != u.key {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.name] = data
		}
		if u.key == "streamdemo" {
			unit := loadUnit(t, root, u.paths)
			feed := unit.Tables["Feed"]
			var id *ir.Field
			for _, f := range feed.Fields {
				if f.Name == "id" {
					id = f
				}
			}
			if id == nil {
				t.Fatal("streamdemo Feed declares no field id")
			}
			wid := ir.TableFieldWireId(id)
			kind := byte(ir.TableWireFieldKind(id))
			wire := []byte{1}
			wire = append(wire, 1, kind, 1, 0, 0, 0)
			wire = append(wire, 1, kind, 2, 0, 0, 0)
			wire = append(wire, 0)
			for i := 0; i < 8; i++ {
				wire = append(wire, byte(wid>>(8*i)))
			}
			wire = append(wire, 1, 0, 0, 0, 0, 0, 0, 0)
			inputs["dup"] = wire

			// an unknown field id in Feed's body: skipped by its kind (uint32),
			// counted unknown (§4, §11)
			const unknownId = uint64(0x1122334455667788)
			uw := []byte{1, 1, 8, 0, 0, 0, 0, 0}
			for i := 0; i < 8; i++ {
				uw = append(uw, byte(unknownId>>(8*i)))
			}
			uw = append(uw, 1, 0, 0, 0, 0, 0, 0, 0)
			inputs["unknown"] = uw

			src, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/stream_chain.bin"))
			if err != nil {
				t.Fatal(err)
			}
			inputs["integrity"] = append(append([]byte{}, src...), 0, 0, 0, 0)
		}
	}

	var p strings.Builder
	p.WriteString(refusalsPrelude)
	for _, c := range cases {
		form := "null"
		if c.form >= 0 {
			form = fmt.Sprintf("%d", c.form)
		}
		if _, ok := inputs[c.input]; !ok {
			inputs[c.input] = nil
		}
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, %s, %t);\n", c.unit, mods[c.unit], c.root, c.input, form, c.integrity)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := sqrun.Run(t, dir, "main.nut", inputs)
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		k, reason, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q is not a case and a reason", line)
		}
		got[k] = reason
	}
	for _, c := range cases {
		if got[c.input] != c.want {
			t.Errorf("%s: strict read refuses %q, want %q", c.key, got[c.input], c.want)
		}
	}
}
