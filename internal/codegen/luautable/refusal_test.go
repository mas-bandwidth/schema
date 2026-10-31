package luautable_test

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refusalPrelude is the strict-read probe: every case requires the table
// module that declares its root, optionally forges a one-byte file of a given
// form byte, calls ReadStrict and prints its named reason, the field it names
// and the two counters that do not refuse with theirs (docs/SPEC-TABLES.md
// §6.5, §11, §2.14).
const refusalPrelude = `local inputs = require("./inputs")
local R = require("./schema_refusal")
local function nn(x: any): string
	if x == nil then return "-" end
	return tostring(x)
end
local function run(T: any, root: string, key: string, form: number?)
	local b = inputs[key]
	if form ~= nil then
		b = buffer.create(1)
		buffer.writeu8(b, 0, form)
	end
	local report: any = {}
	local v = R.ReadStrict(T, root, b, report)
	if (v == nil) ~= (report.reason ~= nil) then
		error("ReadStrict answered a value and a reason together, or neither: " .. key)
	end
	print(key .. "|" .. (report.reason or "ok") .. "|" .. nn(report.field) .. "|" ..
		report.unknown .. ":" .. nn(report.unknownField) .. "|" ..
		report.duplicate .. ":" .. nn(report.duplicateField))
end
-- a form-3 file of n fresh records written by schema_fixed.luau's own save (§3.4),
-- with its header's layout hash flipped when asked
local F = require("./schema_fixed")
local S = require("./schema_tables")
local function fixedFile(T: any, root: string, key: string, n: number, flip: boolean, layout: boolean)
	local fixed = { hash = T[root .. "FixedHash"], recordBytes = T[root .. "FixedRecordBytes"], layout = T[root .. "FixedLayout"] }
	local values = {}
	for i = 1, n do values[i] = S.lookup(root).new() end
	local b = F.save(T[root .. "Descriptor"], fixed, values)
	if flip then buffer.writeu8(b, 8, bit32.bxor(buffer.readu8(b, 8), 1)) end
	if layout then buffer.writeu8(b, 20, bit32.bxor(buffer.readu8(b, 20), 1)) end
	local report: any = {}
	local v = R.ReadStrict(T, root, b, report)
	if (v == nil) ~= (report.reason ~= nil) then
		error("ReadStrict answered a value and a reason together, or neither: " .. key)
	end
	print(key .. "|" .. (report.reason or "ok") .. "|-|0:-|0:-")
end
`

type refusalCase struct {
	key    string
	unit   string
	root   string
	input  string
	form   int    // -1 for the input's own bytes
	saved  int    // > 0: a form-3 file of saved-1 records from schema_fixed.luau's save
	flip   bool   // with saved: the header's layout hash flipped
	layout bool   // with saved: the layout's entry count flipped
	side   string // "unknown:<field>" or "duplicate:<field>" the report must still count
	want   string
	field  string // "" for any field; "-" for none
}

// strictName is the one name §6.5 gives a pinned report (u,k,w,c,d,m,verdict
// of testdata/conformance/tables/reports.txt) under ReadStrict's order, Go's
// TableReport.Err(): malformed, then clamped, then kind_mismatch. A malformed
// report the reference did not read is the file's own framing, wire_damaged.
// unknown, widened and duplicate alone are not refusals (§6.5). "ok" is a
// clean read.
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
// at a root, or "" when the root reads the form (docs/SPEC-TABLES.md, The
// three wires; §3, §3.4, §6.5): a declared fixed root reads form 3 through its
// own fixed reader (whose names are its own) and refuses form 1 as
// previous_form; a variable root reads form 1 and refuses form 3 as
// newer_form; form 2 is message_form_as_file at either; any other byte is
// newer_form.
func formName(b byte, fixed bool) string {
	switch {
	case b == 1 && !fixed, b == 3 && fixed:
		return ""
	case b == 1:
		return "previous_form"
	case b == 2:
		return "message_form_as_file"
	}
	return "newer_form"
}

// emittedRoot is what a <Base>Table.luau module says of a root it declares:
// its class, and whether a fixed layout was laid down for it (fixedform.go).
type emittedRoot struct {
	module string
	fixed  bool
	layout bool
}

var descriptorRe = regexp.MustCompile(`(?m)^_T\.(\w+)Descriptor = _schema_tables\.table\(\{\n\tname = "(\w+)",\n(?:\t.*\n)*?\ttable = true,\n\tfixed = (true|false),`)

// emittedRoots reads every TABLE descriptor (table = true: a packet type or a
// map entry a table reaches is not a root) and fixed layout the unit's modules
// emit.
func emittedRoots(g *generatedUnit) map[string]emittedRoot {
	out := map[string]emittedRoot{}
	for name, data := range g.files {
		if !strings.HasSuffix(name, "Table.luau") {
			continue
		}
		mod := strings.TrimSuffix(name, ".luau")
		for _, m := range descriptorRe.FindAllStringSubmatch(string(data), -1) {
			out[m[2]] = emittedRoot{
				module: mod,
				fixed:  m[3] == "true",
				layout: strings.Contains(string(data), "_T."+m[2]+"FixedLayout = "),
			}
		}
	}
	return out
}

// emittedIntegrity requires the emitted descriptor of root to carry the
// table line's `integrity = "crc32c"` (docs/SPEC-TABLES.md §2.14): the strict
// read checks the trailer by that entry, so a unit that declares the key and
// emits no entry would read its trailer as framing damage.
func emittedIntegrity(t *testing.T, g *generatedUnit, root string) {
	t.Helper()
	anchor := fmt.Sprintf("_T.%sDescriptor = _schema_tables.table({\n", root)
	for _, data := range g.files {
		i := strings.Index(string(data), anchor)
		if i < 0 {
			continue
		}
		end := strings.Index(string(data[i:]), "\n})")
		if end < 0 || !strings.Contains(string(data[i:i+end]), "\tintegrity = \"crc32c\",\n") {
			t.Fatalf("%sDescriptor declares integrity = \"crc32c\" in its schema but the emitted descriptor carries no integrity entry", root)
		}
		return
	}
	t.Fatalf("no emitted module declares %sDescriptor", root)
}

// idTable is a form-1 file's id table: the ids, then their count (§3).
func idTable(ids ...uint64) []byte {
	var b []byte
	for _, x := range append(ids, uint64(len(ids))) {
		for i := range 8 {
			b = append(b, byte(x>>(8*i)))
		}
	}
	return b
}

// TestLuauStrictReadRefusesByName drives ReadStrict over EVERY report row of
// the tables manifest whose unit the Luau table surface generates, and the
// name each must answer is derived from the row's pinned counters and its form
// byte, so each counter's nonzero rows are cases (docs/SPEC-TABLES.md §6.5,
// §11). A counter refusal must also name a field, and the rows whose field the
// schema states are pinned to it. Beside them: a constructed repeated map key
// and an unknown field, which READ and are still counted with their fields;
// the integrity trailer both ways and its absence (§2.14); the file's own
// framing three ways; and one forged form byte per form refusal, at a variable
// root, at a fixed root with its reader, and at a fixed root without one.
func TestLuauStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := manifestRows(t)
	pinned := reportsExpect(t)

	gens := map[string]*generatedUnit{}
	roots := map[string]map[string]emittedRoot{}
	unitOf := func(key string) *generatedUnit {
		g, ok := gens[key]
		if !ok {
			g = generateUnit(t, units, key)
			gens[key] = g
			if g.err == nil {
				roots[key] = emittedRoots(g)
			}
		}
		return g
	}

	inputs := map[string]map[string][]byte{}
	put := func(unit, key string, data []byte) {
		if inputs[unit] == nil {
			inputs[unit] = map[string][]byte{}
		}
		inputs[unit][key] = data
	}

	var cases []refusalCase
	movedBy := map[string]int{}
	// the first field the walk saw, pinned for EVERY counter row: the field
	// (or union arm, the innermost holder) the row's schema pair moves, and a
	// new counter row fails until its field is pinned here
	pinnedFields := map[string]string{
		"p1_as_p2":                 "link",
		"p3_as_p2":                 "link",
		"parts_elem_kind":          "parts",
		"parts_count_past_bound":   "parts",
		"parts_index_out_of_range": "parts",
		// mutant 235: the root's first field reference 1 (one) bumped to 11
		// (past): a table body under past's int32 id is past's kind_mismatch
		"map_depth_root_kind": "past",
	}
	compared := 0
	pinUsed := map[string]bool{}
	for _, r := range rows {
		if r.kind != "report" {
			continue
		}
		if _, ok := units[r.unit]; !ok {
			continue
		}
		if g := unitOf(r.unit); g.err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		p, ok := pinned[r.caseName]
		if !ok {
			t.Fatalf("report row %s has no pinned report", r.caseName)
		}
		er, ok := roots[r.unit][r.root]
		if !ok {
			t.Fatalf("report row %s: unit %s emits no descriptor %s", r.caseName, r.unit, r.root)
		}
		put(r.unit, r.caseName, data)
		want := strictName(t, r.caseName, p)
		field := pinnedFields[r.caseName]
		switch {
		case len(data) == 0:
			want = "wire_damaged"
		case formName(data[0], er.fixed) != "":
			want, field = formName(data[0], er.fixed), "-"
		case data[0] == 3:
			t.Fatalf("%s: a form-3 report row; the fixed reader's name is not derived here", r.caseName)
		default:
			movedBy[want]++
			pinUsed[r.caseName] = true
			if want != "ok" && field == "" {
				t.Errorf("%s refuses as %s and its field is not pinned: pin it in pinnedFields", r.caseName, want)
			}
		}
		if want == "ok" || want == "wire_damaged" {
			field = "-"
		}
		cases = append(cases, refusalCase{key: r.caseName, unit: r.unit, root: r.root, input: r.caseName, form: -1, want: want, field: field})
		compared++
	}
	if compared == 0 {
		t.Fatal("no report row reached a generated unit")
	}
	// a pin no counter row reads is stale: its row now answers a form byte
	// (a declared fixed root's form-1 file is previous_form) or reads clean
	for k := range pinnedFields {
		if !pinUsed[k] {
			t.Errorf("pinnedFields[%q] is read by no counter row: drop it", k)
		}
	}
	// every counter §6.5 refuses on does so on at least one pinned row
	for _, name := range []string{"kind_mismatch", "clamped", "malformed"} {
		if movedBy[name] == 0 {
			t.Errorf("no generated report row refuses as %s", name)
		}
	}

	// streamdemo's Feed: a variable root with a uint32 field `id`
	if g := unitOf("streamdemo"); g.err != nil {
		t.Fatalf("streamdemo does not generate: %v", g.err)
	}
	paths, err := compiler.GatherPaths(prefix(units["streamdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	feed := u.Tables["Feed"]
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
	// a known field, then an id Feed cannot name: skipped by its kind, counted
	// unknown, and named by its wire id since it has no name (§4, §11)
	const unknownId = uint64(0x1122334455667788)
	put("streamdemo", "unknown", append([]byte{1, 1, kind, 7, 0, 0, 0, 2, 8, 0, 0, 0, 0, 0}, idTable(wid, unknownId)...))
	// a clean file of Feed, and the same file with a CRC-32C trailer (§2.14)
	clean := append([]byte{1, 1, kind, 9, 0, 0, 0, 0}, idTable(wid)...)
	good := crc32.Checksum(clean, crc32.MakeTable(crc32.Castagnoli))
	sealed := append(append([]byte{}, clean...), byte(good), byte(good>>8), byte(good>>16), byte(good>>24))
	put("streamdemo", "clean", clean)
	put("streamdemo", "sealed", sealed)
	// a root body whose terminator comes before its own end: framing damage
	put("streamdemo", "early_end", append([]byte{1, 0, 1, kind, 9, 0, 0, 0, 0}, idTable(wid)...))
	put("streamdemo", "empty", []byte{})
	// an id table whose count reads negative (past 2^63): the framing's damage,
	// refused before the load walks past the buffer
	put("streamdemo", "negative_count", []byte{1, 0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	// a table that declares the trailer, with the same field, so Feed's wire
	// is its wire (§2.14)
	seal := generateFiles(t, map[string]string{"Seal.schema": "package seal\n\ntable Ledger | integrity = \"crc32c\"\n{\n    id uint32\n}\n"})
	emittedIntegrity(t, seal, "Ledger")
	gens["seal"], roots["seal"] = seal, emittedRoots(seal)
	broken := append([]byte{}, sealed...)
	broken[len(broken)-1] ^= 0x01
	put("seal", "sealed", sealed)
	put("seal", "broken", broken)
	put("seal", "short", []byte{1, 0, 0, 0})

	// a repeated map key: last wins and is counted duplicate (§2.8, §4), and
	// the field is the map's
	dup := generateFiles(t, map[string]string{"Dup.schema": "package dup\ntable Box\n{\n    m map[uint8]uint8\n}\n"})
	gens["dup"], roots["dup"] = dup, emittedRoots(dup)
	put("dup", "dup", duplicateMapWire(t))

	// a fixed root the Luau fixed form carries, and a declared fixed root it
	// lays no layout down for, from the generated corpus: a root is a table
	// the unit DECLARES (a map's entry rides as a table body, and is none)
	var fixedUnit, fixedRoot, bareUnit, bareRoot string
	var keys []string
	for k := range roots {
		if _, ok := units[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		paths, err := compiler.GatherPaths(prefix(units[k]))
		if err != nil {
			t.Fatal(err)
		}
		uk, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for n, st := range uk.Tables {
			if st.FixedDeclared && st.MapEntryOf == "" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			er, ok := roots[k][n]
			if !ok || !er.fixed {
				t.Fatalf("unit %s: the declared fixed table %s has no fixed descriptor", k, n)
			}
			if er.layout && fixedRoot == "" {
				fixedUnit, fixedRoot = k, n
			}
			if !er.layout && bareRoot == "" {
				bareUnit, bareRoot = k, n
			}
		}
	}
	if fixedRoot == "" {
		t.Fatal("no generated unit declares a fixed table the Luau fixed reader carries")
	}

	cases = append(cases,
		// unknown and a repeated key alone READ (§6.5, §4), and the report
		// still counts each with its field
		refusalCase{key: "unknown", unit: "streamdemo", root: "Feed", input: "unknown", form: -1, want: "ok", field: "-", side: fmt.Sprintf("unknown:#%016x", unknownId)},
		refusalCase{key: "duplicate", unit: "dup", root: "Box", input: "dup", form: -1, want: "ok", field: "-", side: "duplicate:m"},
		refusalCase{key: "clean", unit: "streamdemo", root: "Feed", input: "clean", form: -1, want: "ok", field: "-"},
		// §2.14: the trailer checked before any field, both ways, and too short
		refusalCase{key: "integrity_ok", unit: "seal", root: "Ledger", input: "sealed", form: -1, want: "ok", field: "-"},
		refusalCase{key: "integrity_mismatch", unit: "seal", root: "Ledger", input: "broken", form: -1, want: "integrity_mismatch", field: "-"},
		refusalCase{key: "integrity_short", unit: "seal", root: "Ledger", input: "short", form: -1, want: "integrity_mismatch", field: "-"},
		// a trailer read by a table without the key is §2.14's mismatched
		// reader: the count runs past the front of the file, the framing's damage
		refusalCase{key: "integrity_unexpected", unit: "streamdemo", root: "Feed", input: "sealed", form: -1, want: "wire_damaged", field: "-"},
		// the file's own framing: empty, too short for the id table, and a root
		// body that ends before the id table does (Go's tableOpen damage)
		refusalCase{key: "variable_empty", unit: "streamdemo", root: "Feed", input: "empty", form: -1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_short", unit: "streamdemo", root: "Feed", input: "empty", form: 1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_negative_count", unit: "streamdemo", root: "Feed", input: "negative_count", form: -1, want: "wire_damaged", field: "-"},
		refusalCase{key: "variable_early_end", unit: "streamdemo", root: "Feed", input: "early_end", form: -1, want: "wire_damaged", field: "-"},
		// a variable root: form 2 is a message, form 3 and anything else newer
		refusalCase{key: "variable_message", unit: "streamdemo", root: "Feed", input: "empty", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "variable_fixed", unit: "streamdemo", root: "Feed", input: "empty", form: 3, want: "newer_form", field: "-"},
		refusalCase{key: "variable_unknown", unit: "streamdemo", root: "Feed", input: "empty", form: 127, want: "newer_form", field: "-"},
		// a fixed root: form 1 is previous_form, named by the form byte
		// before anything behind it (The three wires, §3.4), form 2 a
		// message, an unknown byte newer
		refusalCase{key: "fixed_variable_form", unit: fixedUnit, root: fixedRoot, input: "empty", form: 1, want: "previous_form", field: "-"},
		refusalCase{key: "fixed_message", unit: fixedUnit, root: fixedRoot, input: "empty", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "fixed_newer", unit: fixedUnit, root: fixedRoot, input: "empty", form: 9, want: "newer_form", field: "-"},
		// a form-3 byte at a fixed root goes to its own fixed reader, which
		// names a file shorter than the header truncated (§5.3, §3.4)
		refusalCase{key: "fixed_own_reader", unit: fixedUnit, root: fixedRoot, input: "empty", form: 3, want: "truncated", field: "-"},
		// a whole form-3 file its own save wrote reads; a strict read answers
		// ONE root, so none or two records are the file's damage; a layout
		// hash this reader holds no plan for is the reader's layout_unknown
		refusalCase{key: "fixed_one_record", unit: fixedUnit, root: fixedRoot, input: "fixed_one_record", form: -1, saved: 2, want: "ok", field: "-"},
		refusalCase{key: "fixed_no_record", unit: fixedUnit, root: fixedRoot, input: "fixed_no_record", form: -1, saved: 1, want: "wire_damaged", field: "-"},
		refusalCase{key: "fixed_two_records", unit: fixedUnit, root: fixedRoot, input: "fixed_two_records", form: -1, saved: 3, want: "wire_damaged", field: "-"},
		refusalCase{key: "fixed_layout_unknown", unit: fixedUnit, root: fixedRoot, input: "fixed_layout_unknown", form: -1, saved: 2, flip: true, want: "layout_unknown", field: "-"},
		refusalCase{key: "fixed_layout_malformed", unit: fixedUnit, root: fixedRoot, input: "fixed_layout_malformed", form: -1, saved: 2, layout: true, want: "layout_malformed", field: "-"},
	)
	// a declared fixed root with no Luau fixed layout: form 3 is not carried
	// by this call, unknown_form, and its form 1 is still previous_form: the
	// class is the declaration's, not the layout's. A record past §3.4's 65536-byte ceiling is one
	// by construction: the fixed form is not emitted for it and the class is
	// kept; the corpus's own, when it has one, is a case too
	big := generateFiles(t, map[string]string{"Big.schema": "package big\n\nfixed table Big\n{\n    a [70000]uint8\n}\n"})
	gens["big"], roots["big"] = big, emittedRoots(big)
	if er := roots["big"]["Big"]; !er.fixed || er.layout {
		t.Fatalf("Big is not a declared fixed root without a fixed layout: %+v", er)
	}
	cases = append(cases,
		refusalCase{key: "bare_fixed_form3", unit: "big", root: "Big", input: "empty", form: 3, want: "unknown_form", field: "-"},
		refusalCase{key: "bare_fixed_form1", unit: "big", root: "Big", input: "empty", form: 1, want: "previous_form", field: "-"},
	)
	if bareRoot != "" {
		cases = append(cases, refusalCase{key: "corpus_bare_fixed_form3", unit: bareUnit, root: bareRoot, input: "empty", form: 3, want: "unknown_form", field: "-"})
	}
	for _, c := range cases {
		if _, ok := inputs[c.unit][c.input]; !ok {
			put(c.unit, c.input, []byte{})
		}
	}

	// one probe per unit, in its own generated directory
	byUnit := map[string][]refusalCase{}
	var order []string
	for _, c := range cases {
		if byUnit[c.unit] == nil {
			order = append(order, c.unit)
		}
		byUnit[c.unit] = append(byUnit[c.unit], c)
	}
	names := map[string]int{}
	for _, unit := range order {
		g := gens[unit]
		var p strings.Builder
		p.WriteString(refusalPrelude)
		mods := map[string]string{}
		for _, c := range byUnit[unit] {
			mod := roots[unit][c.root].module
			if mod == "" {
				t.Fatalf("%s: unit %s emits no module for %s", c.key, unit, c.root)
			}
			if _, ok := mods[mod]; !ok {
				mods[mod] = fmt.Sprintf("T%d", len(mods))
				fmt.Fprintf(&p, "local %s = require(\"./%s\")\n", mods[mod], mod)
			}
		}
		for _, c := range byUnit[unit] {
			if c.saved > 0 {
				fmt.Fprintf(&p, "fixedFile(%s, %q, %q, %d, %t, %t)\n", mods[roots[unit][c.root].module], c.root, c.input, c.saved-1, c.flip, c.layout)
				continue
			}
			form := "nil"
			if c.form >= 0 {
				form = fmt.Sprintf("%d", c.form)
			}
			fmt.Fprintf(&p, "run(%s, %q, %q, %s)\n", mods[roots[unit][c.root].module], c.root, c.input, form)
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(p.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luaurun.Run(t, g.dir, "main.luau", inputs[unit])
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != len(byUnit[unit]) {
			t.Fatalf("unit %s: the probe printed %d lines for %d cases:\n%s", unit, len(lines), len(byUnit[unit]), out)
		}
		for i, c := range byUnit[unit] {
			parts := strings.Split(lines[i], "|")
			if len(parts) != 5 || parts[0] != c.input {
				t.Fatalf("probe line %q is not case %s's input, reason and field", lines[i], c.key)
			}
			reason, field := parts[1], parts[2]
			if reason != c.want {
				t.Errorf("%s: strict read answers %q, want %q (%s)", c.key, reason, c.want, lines[i])
				continue
			}
			names[reason]++
			if field != "-" {
				t.Logf("%s: %s names %s", c.key, reason, field)
			}
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
	}
	for _, name := range []string{"malformed", "wire_damaged", "clamped", "kind_mismatch",
		"message_form_as_file", "newer_form", "previous_form", "unknown_form", "integrity_mismatch", "layout_malformed", "layout_unknown", "ok"} {
		if names[name] == 0 {
			t.Errorf("no case answered %s", name)
		}
	}
	t.Logf("%d report rows compared, %d cases in all; names %v; fixed root %s.%s, bare fixed root %q", compared, len(cases), names, fixedUnit, fixedRoot, bareRoot)
}

// TestLuauStrictReadNamesCoverEveryReason holds schema_refusal.luau's ReadStrictNames
// to the reasons the strict read can answer (docs/SPEC-TABLES.md §11, §6.5):
// every reason schema_refusal.luau sets, every counter name it maps, and every name
// the fixed reader raises through it (schema_fixed.luau, §3.4) is a member, and
// every member is one of them.
func TestLuauStrictReadNamesCoverEveryReason(t *testing.T) {
	t.Parallel()
	rt := luautable.Runtime()
	refusal, fixed := string(rt["schema_refusal.luau"]), string(rt["schema_fixed.luau"])
	load := string(rt["schema_load.luau"])
	if refusal == "" || fixed == "" || load == "" {
		t.Fatal("the Luau table runtime emits no schema_refusal.luau, schema_fixed.luau or schema_load.luau")
	}
	answeredText := refusal + "\n" + load
	start := strings.Index(refusal, "R.ReadStrictNames = table.freeze({")
	if start < 0 {
		t.Fatal("schema_refusal.luau defines no R.ReadStrictNames")
	}
	end := strings.Index(refusal[start:], "})")
	block := refusal[start : start+end]
	members := map[string]bool{}
	for _, m := range regexp.MustCompile(`([a-z_]+) = true`).FindAllStringSubmatch(block, -1) {
		members[m[1]] = true
	}
	answered := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`reason = "([a-z_]+)"`),
		regexp.MustCompile(`(?m)^\t[a-zA-Z]+ = "([a-z_]+)",$`), // the NAMES map
	} {
		for _, m := range re.FindAllStringSubmatch(answeredText, -1) {
			answered[m[1]] = true
		}
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`refuse\("([a-z_]+)"(?:, true)?\)`),
		regexp.MustCompile(`\{ name = "([a-z_]+)" \}`),
		// F.read's own refusals, answered through its report (§5.3)
		regexp.MustCompile(`no\("([a-z_]+)"(?:, true)?\)`),
	} {
		for _, m := range re.FindAllStringSubmatch(fixed, -1) {
			answered[m[1]] = true
		}
	}
	if len(answered) == 0 || len(members) == 0 {
		t.Fatalf("found %d answered reasons and %d members", len(answered), len(members))
	}
	for n := range answered {
		if !members[n] {
			t.Errorf("the strict read can answer %s, but ReadStrictNames does not name it", n)
		}
	}
	for n := range members {
		if !answered[n] {
			t.Errorf("ReadStrictNames names %s, but nothing the strict read reaches answers it", n)
		}
	}
}
