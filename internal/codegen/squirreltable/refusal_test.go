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
local run = function(unit, mod, root, key, form, integrity) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local b = inputs[key];
  if (form != null) { b = blob(1); b.writen(form, 'b'); }
  local d = T.Descriptors[root];
  if (integrity) { d.integrity <- "crc32c"; } else if ("integrity" in d) { delete d.integrity; }
  local report = {};
  local v = R.ReadStrict(T, root, b, report);
  if ((v == null) != (report.reason != null)) throw "ReadStrict answered a value and a reason together, or neither";
  local nn = function(x) { return x == null ? "-" : x; };
  print(key + "|" + (report.reason == null ? "ok" : report.reason) + "|" + nn(report.field) + "|" +
        report.unknown + ":" + nn(report.unknownField) + "|" +
        report.duplicate + ":" + nn(report.duplicateField) + "\n");
};
`

type refusalCase struct {
	key       string
	unit      string
	root      string
	input     string
	form      int // -1 for the input's own bytes
	integrity bool
	side      string // for a read: "unknown:<field>" or "duplicate:<field>" it must still count
	want      string
	field     string // "" for any field; "-" for none
}

// strictName is the one name §6.5 gives a pinned report (u,k,w,c,d,m,verdict
// of testdata/conformance/tables/reports.txt) under ReadStrict's order, Go's
// TableReport.Err(): malformed, then clamped, then kind_mismatch. unknown,
// widened and duplicate alone are not refusals (§6.5: unknown is evolution,
// widened decoded exactly). "ok" is a clean read.
func strictName(t *testing.T, row, pinned string) string {
	t.Helper()
	f := strings.Split(pinned, ",")
	if len(f) != 7 {
		t.Fatalf("%s: pinned report %q is not u,k,w,c,d,m,verdict", row, pinned)
	}
	switch {
	case f[5] == "true" && f[6] == "read":
		return "malformed"
	case f[5] == "true":
		return "wire_damaged"
	case f[3] != "0":
		return "clamped"
	case f[1] != "0":
		return "kind_mismatch"
	}
	return "ok"
}

// formName is ReadStrict's form-byte refusal for a file whose first byte is b
// at a root with a fixed reader or not (a declared fixed root the Squirrel
// fixed form does not carry is read as form 1; and ReadStrict at a
// declared-fixed root reads a form-1 file as the variable form, the accepting
// read) (docs/SPEC-TABLES.md, The three wires: neither class borrows the
// other's form; §6.5, the ruling), or "" when the form is the root's own.
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
	// the first field the walk saw, pinned exactly where the schema says it
	// (the wire files are named for it); every other counter row must name one
	pinnedFields := map[string]string{
		"parts_elem_kind":         "parts",
		"parts_count_past_bound":  "parts",
		"trace_elem_kind":         "trace",
		"trace_count_past_bound":  "trace",
		"a2_arm_length_off_width": "b",
	}
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
		want := strictName(t, r.name, p)
		field := pinnedFields[r.name]
		if len(data) == 0 {
			want = "wire_damaged"
		} else if f := formName(data[0], isFixed(r.unit, r.root) && fixedReaders[r.unit+"/"+r.root]); f != "" {
			want, field = f, "-"
		} else if data[0] == 3 {
			t.Fatalf("%s: a form-3 report row; the fixed reader's name is not derived here", r.name)
		} else {
			moved[want]++
		}
		if want == "ok" || want == "wire_damaged" {
			field = "-"
		}
		cases = append(cases, refusalCase{key: r.name, unit: r.unit, root: r.root, input: r.name, form: -1, want: want, field: field})
		compared++
	}
	if compared == 0 {
		t.Fatal("no report row reached a generated unit")
	}
	// every counter §6.5 refuses on does so on at least one pinned row
	for _, name := range []string{"kind_mismatch", "clamped", "malformed"} {
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
	// a root body whose terminator comes before its own end: framing damage
	early := []byte{1, 0, 1, kind, 9, 0, 0, 0, 0}
	inputs["early_end"] = append(early, idTable(wid)...)

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
	// a declared fixed root the Squirrel fixed form does not carry
	bareUnit, bareRoot := "", ""
	for _, u := range units {
		if refused[u.key] {
			continue
		}
		for name, st := range parsed[u.key].Tables {
			if st.FixedDeclared && !fixedReaders[u.key+"/"+name] && (bareRoot == "" || u.key+name < bareUnit+bareRoot) {
				bareUnit, bareRoot = u.key, name
			}
		}
	}
	if fixedRoot == "" {
		t.Fatal("no generated unit declares a fixed table the Squirrel fixed reader carries")
	}

	cases = append(cases,
		// duplicate and unknown alone READ (§6.5, §4: last wins; unknown is
		// evolution), and the report still counts each with its field
		refusalCase{key: "duplicate", unit: "streamdemo", root: "Feed", input: "dup", form: -1, want: "ok", field: "-", side: "duplicate:id"},
		refusalCase{key: "unknown", unit: "streamdemo", root: "Feed", input: "unknown", form: -1, want: "ok", field: "-", side: fmt.Sprintf("unknown:#%016x", unknownId)},
		refusalCase{key: "clean", unit: "streamdemo", root: "Feed", input: "clean_plain", form: -1, want: "ok", field: "-"},
		refusalCase{key: "integrity_ok", unit: "streamdemo", root: "Feed", input: "integrity_ok", form: -1, integrity: true, want: "ok", field: "-"},
		refusalCase{key: "integrity_mismatch", unit: "streamdemo", root: "Feed", input: "integrity_bad", form: -1, integrity: true, want: "integrity_mismatch", field: "-"},
		refusalCase{key: "integrity_short", unit: "streamdemo", root: "Feed", input: "integrity_short", form: -1, integrity: true, want: "integrity_mismatch", field: "-"},
		// a trailer read by a table without the key is §2.14's mismatched
		// reader: the count runs past the front of the file, the framing's damage
		refusalCase{key: "integrity_unexpected", unit: "streamdemo", root: "Feed", input: "integrity_ok", form: -1, want: "wire_damaged", field: "-"},
		// the file's own framing: empty, too short for the id table, and a root
		// body that ends before the id table does (Go's tableOpen damage)
		refusalCase{key: "variable_empty", unit: "streamdemo", root: "Feed", input: "empty", form: -1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_short", unit: "streamdemo", root: "Feed", input: "form_v1", form: 1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_early_end", unit: "streamdemo", root: "Feed", input: "early_end", form: -1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_message", unit: "streamdemo", root: "Feed", input: "form_v2", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "variable_fixed", unit: "streamdemo", root: "Feed", input: "form_v3", form: 3, want: "newer_form", field: "-"},
		refusalCase{key: "variable_unknown", unit: "streamdemo", root: "Feed", input: "form_v127", form: 127, want: "newer_form", field: "-"},
		// a fixed root: form 1 is READ as the variable form (the ruling, §6.5);
		// a lone form byte is the framing's damage, not previous_form
		refusalCase{key: "fixed_previous", unit: fixedUnit, root: fixedRoot, input: "form_f1", form: 1, want: "wire_damaged", field: "-"},
		refusalCase{key: "fixed_message", unit: fixedUnit, root: fixedRoot, input: "form_f2", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "fixed_newer", unit: fixedUnit, root: fixedRoot, input: "form_f9", form: 9, want: "newer_form", field: "-"},
		// a form-3 byte at a fixed root goes to its own fixed reader, which
		// names a header too short to hold a layout (§3.4)
		refusalCase{key: "fixed_own_reader", unit: fixedUnit, root: fixedRoot, input: "form_f3", form: 3, want: "layout_malformed", field: "-"},
	)
	if bareRoot != "" {
		// a declared fixed root with no Squirrel fixed reader: form 3 is not
		// carried by this call, and its form-1 framing is read (a one-byte
		// file is the framing's damage, not previous_form)
		cases = append(cases,
			refusalCase{key: "bare_fixed_form3", unit: bareUnit, root: bareRoot, input: "form_b3", form: 3, want: "unknown_form", field: "-"},
			refusalCase{key: "bare_fixed_form1", unit: bareUnit, root: bareRoot, input: "form_b1", form: 1, want: "wire_damaged", field: "-"},
		)
	}
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
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, %s, %t);\n", c.unit, mods[c.unit], c.root, c.input, form, c.integrity)
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
		if len(parts) != 5 || parts[0] != c.input {
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
		case c.field == "" && field == "-":
			t.Errorf("%s: %s names no field", c.key, reason)
		}
		if c.side != "" {
			counter, f, _ := strings.Cut(c.side, ":")
			got := parts[3]
			if counter == "duplicate" {
				got = parts[4]
			}
			if got != "1:"+f {
				t.Errorf("%s: the report counts %s as %q, want %q", c.key, counter, got, "1:"+f)
			}
		}
	}
	for _, name := range []string{"malformed", "wire_damaged", "clamped", "kind_mismatch",
		"message_form_as_file", "newer_form", "integrity_mismatch", "ok"} {
		if names[name] == 0 {
			t.Errorf("no case answered %s", name)
		}
	}
	t.Logf("%d report rows compared, %d cases in all; names %v", compared, len(cases), names)
}

// strictReadFormOnePrelude is the form-1-at-fixed-root probe: ReadStrict over
// one wire, printing whether it answered the root or null, its reason, and the
// §4 counters (docs/SPEC-TABLES.md §6.5, §4).
const strictReadFormOnePrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local report = {};
  local v = R.ReadStrict(T, root, inputs[key], report);
  print(key + "|" + (v == null ? "null" : "root") + "|" +
        (report.reason == null ? "-" : report.reason) + "|" +
        report.unknown + "," + report.kindMismatch + "," + report.widened + "," +
        report.clamped + "," + report.duplicate + "\n");
};
`

// TestSquirrelStrictReadReadsFormOneAtFixedRoot pins the ruling: ReadStrict at
// a declared-fixed root READS a form-1 file as the variable form, so the
// v1_cfg_as_v2 corpus row (tblv2 Cfg over the v1_cfg wire file) is no longer
// refused as previous_form. Its report matches the row's counters in reports.txt
// (docs/SPEC-TABLES.md §6.5, the ruling).
func TestSquirrelStrictReadReadsFormOneAtFixedRoot(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var paths []string
	for _, u := range units {
		if u.key == "tblv2" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tblv2 unit")
	}
	unit := loadUnit(t, root, paths)
	if cfg := unit.Tables["Cfg"]; cfg == nil || !cfg.FixedDeclared {
		t.Fatal("tblv2 Cfg is not a declared-fixed root")
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "v1_cfg.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[0] != 1 {
		t.Fatalf("v1_cfg.bin carries form byte %d, want 1 (the variable form)", data[0])
	}
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))["v1_cfg_as_v2"]
	const wantReport = "1,1,0,1,0,false,read"
	if pinned != wantReport {
		t.Fatalf("v1_cfg_as_v2 pinned report %q, want %q", pinned, wantReport)
	}
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("tblv2: the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("tblv2: the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tblv2")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, d := range files {
		writeFile(t, filepath.Join(out, name), d)
	}
	for name, d := range tables {
		writeFile(t, filepath.Join(out, name), d)
	}
	mod := squirreltable.ModuleName(unit)
	var p strings.Builder
	p.WriteString(strictReadFormOnePrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "tblv2", mod, "Cfg", "v1cfg")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	stdout := sqrun.Run(t, dir, "main.nut", map[string][]byte{"v1cfg": data})
	parts := strings.Split(strings.TrimRight(string(stdout), "\n"), "|")
	if len(parts) != 4 || parts[0] != "v1cfg" {
		t.Fatalf("probe line %q is not key|value|reason|counters", string(stdout))
	}
	reason := parts[2]
	if reason == "previous_form" {
		t.Errorf("ReadStrict at a declared-fixed root refuses a form-1 file as previous_form; the ruling reads it")
	}
	if want := strictName(t, "v1_cfg_as_v2", pinned); reason != want {
		t.Errorf("ReadStrict at a declared-fixed root refuses %q, want %q (the variable-form read's own strict name)", reason, want)
	}
	if (reason == "-") != (parts[1] == "root") {
		t.Errorf("ReadStrict answered %q and %q together; it answers one or the other", parts[1], reason)
	}
	wantCounters := wantReport[:len(wantReport)-len(",false,read")]
	if parts[3] != wantCounters {
		t.Errorf("the read reports %q, want the row's counters %q", parts[3], wantCounters)
	}
	t.Logf("v1_cfg_as_v2 at a declared-fixed root: value %s, reason %s, counters %s", parts[1], reason, parts[3])
}
