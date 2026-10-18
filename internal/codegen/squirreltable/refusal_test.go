package squirreltable_test

import (
	"fmt"
	"hash/crc32"
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
// the loader, optionally forges a one-byte file of a given form byte,
// optionally declares integrity on the root descriptor, then calls ReadStrict
// and prints its named reason and the field it names (docs/SPEC-TABLES.md
// §6.5, §11, §2.14).
const refusalsPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key, form, integrity, fixedEntry) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local b = inputs[key];
  if (form != null) { b = blob(1); b.writen(form, 'b'); }
  local d = T.Descriptors[root];
  if (integrity) { d.integrity <- "crc32c"; } else if ("integrity" in d) { delete d.integrity; }
  local report = {};
  local v = fixedEntry ? R.ReadStrictFixed(T, root, b, report) : R.ReadStrict(T, root, b, report);
  if ((v == null) != (report.reason != null)) throw "ReadStrict answered a value and a reason together, or neither";
  print(key + "|" + (report.reason == null ? "ok" : report.reason) + "|" +
        (report.field == null ? "-" : report.field) + "\n");
};
`

type refusalCase struct {
	key       string
	unit      string
	root      string
	input     string
	form      int // -1 for the input's own bytes
	integrity bool
	fixed     bool // through ReadStrictFixed, the fixed reader's entry
	want      string
	field     string // "" for any field; "-" for none
}

// strictName is the one name §11 gives a pinned report (u,k,w,c,d,m,verdict
// of testdata/conformance/tables/reports.txt) under ReadStrict's order:
// damage first, then duplicate, clamped, unknown, kind_mismatch. widened is
// not a refusal (§4's rung). "" is a clean read.
func strictName(t *testing.T, row, pinned string) (string, string) {
	t.Helper()
	f := strings.Split(pinned, ",")
	if len(f) != 7 {
		t.Fatalf("%s: pinned report %q is not u,k,w,c,d,m,verdict", row, pinned)
	}
	switch {
	case f[5] == "true":
		return "malformed", "malformed"
	case f[4] != "0":
		return "duplicate", "duplicate"
	case f[3] != "0":
		return "clamped", "clamped"
	case f[0] != "0":
		return "unknown", "unknown"
	case f[1] != "0":
		return "kind_mismatch", "kindMismatch"
	}
	return "ok", ""
}

// formName is ReadStrict's form-byte refusal for a file whose first byte is b
// (§3, §6.5): it reads by the form byte, so form 1 is read whatever the
// root's declaration, form 2 is a batch handed to a file root, and every
// other byte but a fixed root's own 3 is newer. "" is a form it reads.
func formName(b byte, fixed bool) string {
	switch {
	case b == 1, fixed && b == 3:
		return ""
	case b == 2:
		return "message_form_as_file"
	}
	return "newer_form"
}

// TestSquirrelStrictReadRefusesByName drives ReadStrict over EVERY report row
// of the manifest whose unit the Squirrel table surface generates, and the
// name each must answer is derived from the row's pinned counters and its form
// byte, so each counter's nonzero rows are cases (docs/SPEC-TABLES.md §6.5,
// §11). A counter refusal must also name a field. Beside them: a constructed
// duplicate and unknown field (their names pinned exactly), the integrity
// trailer both ways (§2.14), and one forged form byte per form refusal in each
// direction, at a variable root and at a fixed root.
func TestSquirrelStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := readReportRows(t, manifest)
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))

	dir := t.TempDir()
	inputs := map[string][]byte{}
	mods := map[string]string{}
	parsed := map[string]*ir.Unit{}
	refused := map[string]bool{}
	fixedReaders := map[string]bool{}
	for _, u := range units {
		unit := loadUnit(t, root, u.paths)
		files, err := squirrel.Generate(unit)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", u.key, err)
		}
		tables, err := squirreltable.Generate(unit)
		if err != nil {
			refused[u.key] = true
			continue
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
			for tname := range unit.Tables {
				if strings.Contains(string(data), "_M."+tname+"FixedLoad <-") {
					fixedReaders[u.key+"/"+tname] = true
				}
			}
		}
		mods[u.key] = squirreltable.ModuleName(unit)
		parsed[u.key] = unit
	}
	isFixed := func(unit, table string) bool {
		st := parsed[unit].Tables[table]
		if st == nil {
			t.Fatalf("unit %s declares no table %s", unit, table)
		}
		return st.FixedDeclared
	}

	var cases []refusalCase
	moved := map[string]int{}
	compared := 0
	for _, r := range rows {
		if refused[r.unit] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
		if err != nil {
			t.Fatal(err)
		}
		p, ok := pinned[r.name]
		if !ok {
			t.Fatalf("report row %s has no pinned report", r.name)
		}
		inputs[r.name] = data
		want, counter := strictName(t, r.name, p)
		field := ""
		if len(data) == 0 {
			want = "malformed"
		} else if f := formName(data[0], isFixed(r.unit, r.root)); f != "" {
			want, counter, field = f, "", "-"
		} else if data[0] == 3 {
			t.Fatalf("%s: a form-3 report row; the fixed reader's name is not derived here", r.name)
		}
		if counter != "" {
			moved[want]++
		}
		if want == "ok" {
			field = "-"
		}
		cases = append(cases, refusalCase{key: r.name, unit: r.unit, root: r.root, input: r.name, form: -1, want: want, field: field})
		compared++
	}
	if compared == 0 {
		t.Fatal("no report row reached a generated unit")
	}
	// p2_as_p1: P1's Chain is a fixed, by-value table, so the node table
	// field a pointered writer puts in the root body (the reserved id
	// 0xffffffffffffffff, §3.1) is a field it cannot name: unknown, and named
	// by that id
	if !refused["tblp1"] && !refused["tblp2"] {
		for i := range cases {
			if cases[i].key == "p2_as_p1" {
				cases[i].field = "#ffffffffffffffff"
			}
		}
	}
	// every counter §11 names refuses on at least one pinned row (unknown,
	// kind_mismatch, clamped, malformed) or on the constructed wire below
	// (duplicate: no manifest row moves it)
	for _, name := range []string{"unknown", "kind_mismatch", "clamped", "malformed"} {
		if moved[name] == 0 {
			t.Errorf("no generated report row refuses as %s", name)
		}
	}

	// streamdemo's Feed: a variable root with a uint32-kind field `id`
	feed := parsed["streamdemo"].Tables["Feed"]
	var id *ir.Field
	for _, f := range feed.Fields {
		if f.Name == "id" {
			id = f
		}
	}
	if id == nil || feed.FixedDeclared {
		t.Fatal("streamdemo Feed is not a variable table declaring field id")
	}
	wid := ir.TableFieldWireId(id)
	kind := byte(ir.TableWireFieldKind(id))
	idTable := func(ids ...uint64) []byte {
		var b []byte
		for _, x := range ids {
			for i := 0; i < 8; i++ {
				b = append(b, byte(x>>(8*i)))
			}
		}
		n := uint64(len(ids))
		for i := 0; i < 8; i++ {
			b = append(b, byte(n>>(8*i)))
		}
		return b
	}
	// the field id twice: last wins, counted duplicate, and the field is id
	dup := []byte{1, 1, kind, 1, 0, 0, 0, 1, kind, 2, 0, 0, 0, 0}
	inputs["dup"] = append(dup, idTable(wid)...)
	// a known field, then an id Feed cannot name: skipped by its kind, counted
	// unknown, and named by its wire id since it has no name (§4, §11)
	const unknownId = uint64(0x1122334455667788)
	unk := []byte{1, 1, kind, 7, 0, 0, 0, 2, 8, 0, 0, 0, 0, 0}
	inputs["unknown"] = append(unk, idTable(wid, unknownId)...)
	// a clean file of Feed with a CRC-32C trailer (§2.14), and the same file
	// with its trailer's last byte flipped
	clean := append([]byte{1, 1, kind, 9, 0, 0, 0, 0}, idTable(wid)...)
	good := crc32.Checksum(clean, crc32.MakeTable(crc32.Castagnoli))
	sealed := append(append([]byte{}, clean...), byte(good), byte(good>>8), byte(good>>16), byte(good>>24))
	inputs["integrity_ok"] = sealed
	broken := append([]byte{}, sealed...)
	broken[len(broken)-1] ^= 0x01
	inputs["integrity_bad"] = broken
	inputs["integrity_short"] = []byte{1, 0, 0, 0}
	inputs["clean_plain"] = clean

	// a fixed root for the forged form bytes at a fixed reader
	fixedUnit, fixedRoot := "", ""
	for _, u := range units {
		if refused[u.key] {
			continue
		}
		for name, st := range parsed[u.key].Tables {
			if st.FixedDeclared && fixedReaders[u.key+"/"+name] && (fixedRoot == "" || u.key+name < fixedUnit+fixedRoot) {
				fixedUnit, fixedRoot = u.key, name
			}
		}
	}
	if fixedRoot == "" {
		t.Fatal("no generated unit declares a fixed table the Squirrel fixed reader carries")
	}

	cases = append(cases,
		refusalCase{key: "duplicate", unit: "streamdemo", root: "Feed", input: "dup", form: -1, want: "duplicate", field: "id"},
		refusalCase{key: "unknown", unit: "streamdemo", root: "Feed", input: "unknown", form: -1, want: "unknown", field: fmt.Sprintf("#%016x", unknownId)},
		refusalCase{key: "clean", unit: "streamdemo", root: "Feed", input: "clean_plain", form: -1, want: "ok", field: "-"},
		refusalCase{key: "integrity_ok", unit: "streamdemo", root: "Feed", input: "integrity_ok", form: -1, integrity: true, want: "ok", field: "-"},
		refusalCase{key: "integrity_mismatch", unit: "streamdemo", root: "Feed", input: "integrity_bad", form: -1, integrity: true, want: "integrity_mismatch", field: "-"},
		refusalCase{key: "integrity_short", unit: "streamdemo", root: "Feed", input: "integrity_short", form: -1, integrity: true, want: "integrity_mismatch", field: "-"},
		// a trailer read by a table without the key is §2.14's mismatched reader:
		// damage by §3's rules, never a value
		refusalCase{key: "integrity_unexpected", unit: "streamdemo", root: "Feed", input: "integrity_ok", form: -1, want: "malformed"},
		refusalCase{key: "variable_message", unit: "streamdemo", root: "Feed", input: "form_v2", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "variable_fixed", unit: "streamdemo", root: "Feed", input: "form_v3", form: 3, want: "newer_form", field: "-"},
		refusalCase{key: "variable_unknown", unit: "streamdemo", root: "Feed", input: "form_v127", form: 127, want: "newer_form", field: "-"},
		refusalCase{key: "variable_empty", unit: "streamdemo", root: "Feed", input: "empty", form: -1, want: "malformed", field: "-"},
		// the same fixed root's form-1 file read BY THE FORM BYTE is the
		// variable file, not a refusal (the C++ reference's reader accepts both)
		refusalCase{key: "fixed_reads_variable", unit: fixedUnit, root: fixedRoot, input: "form_f1", form: 1, want: "malformed", field: "-"},
		// the fixed reader's own entry: form 1 is OLDER, previous_form (§3.4)
		refusalCase{key: "fixed_previous", unit: fixedUnit, root: fixedRoot, input: "form_f1", form: 1, fixed: true, want: "previous_form", field: "-"},
		refusalCase{key: "fixed_message", unit: fixedUnit, root: fixedRoot, input: "form_f2", form: 2, fixed: true, want: "message_form_as_file", field: "-"},
		refusalCase{key: "fixed_newer", unit: fixedUnit, root: fixedRoot, input: "form_f9", form: 9, fixed: true, want: "newer_form", field: "-"},
		refusalCase{key: "fixed_newer_strict", unit: fixedUnit, root: fixedRoot, input: "form_f9", form: 9, want: "newer_form", field: "-"},
		// a form-3 byte at a fixed root goes to its own fixed reader, which
		// names a header too short to hold a layout (§3.4), from both entries
		refusalCase{key: "fixed_own_reader", unit: fixedUnit, root: fixedRoot, input: "form_f3", form: 3, want: "layout_malformed", field: "-"},
		refusalCase{key: "fixed_own_reader_fixed", unit: fixedUnit, root: fixedRoot, input: "form_f3", form: 3, fixed: true, want: "layout_malformed", field: "-"},
	)
	inputs["empty"] = []byte{}

	var p strings.Builder
	p.WriteString(refusalsPrelude)
	for _, c := range cases {
		form := "null"
		if c.form >= 0 {
			form = fmt.Sprintf("%d", c.form)
		}
		if _, ok := inputs[c.input]; !ok {
			inputs[c.input] = []byte{}
		}
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, %s, %t, %t);\n", c.unit, mods[c.unit], c.root, c.input, form, c.integrity, c.fixed)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	out := sqrun.Run(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the probe printed %d lines for %d cases:\n%s", len(lines), len(cases), out)
	}
	names := map[string]int{}
	for i, c := range cases {
		parts := strings.Split(lines[i], "|")
		if len(parts) != 3 || parts[0] != c.input {
			t.Fatalf("probe line %q is not case %s's input, reason and field", lines[i], c.key)
		}
		reason, field := parts[1], parts[2]
		if reason != c.want {
			t.Errorf("%s: strict read refuses %q, want %q", c.key, reason, c.want)
			continue
		}
		names[reason]++
		switch {
		case c.field != "" && field != c.field:
			t.Errorf("%s: %s names field %q, want %q", c.key, reason, field, c.field)
		case c.field == "" && reason != "malformed" && field == "-":
			t.Errorf("%s: %s names no field", c.key, reason)
		}
	}
	for _, name := range []string{"malformed", "duplicate", "clamped", "unknown", "kind_mismatch",
		"previous_form", "message_form_as_file", "newer_form", "integrity_mismatch", "ok"} {
		if names[name] == 0 {
			t.Errorf("no case answered %s", name)
		}
	}
	t.Logf("%d report rows compared, %d cases in all; names %v", compared, len(cases), names)
}
