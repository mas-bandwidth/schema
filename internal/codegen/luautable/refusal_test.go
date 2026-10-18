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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refusalPrelude is the strict-read probe: every case requires the table
// module that declares its root, optionally forges a one-byte file of a given
// form byte, calls ReadStrict and prints its named reason, the field it names
// and the two counters that do not refuse with theirs (docs/SPEC-TABLES.md
// §6.5, §11, §2.14).
const refusalPrelude = `local inputs = require("./inputs")
local R = require("./refusal")
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
`

type refusalCase struct {
	key   string
	unit  string
	root  string
	input string
	form  int    // -1 for the input's own bytes
	side  string // "unknown:<field>" or "duplicate:<field>" the report must still count
	want  string
	field string // "" for any field; "-" for none
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
// three wires; §3, §3.4, §6.5): form 1 is read at every root, the declared
// fixed ones included; form 3 belongs to a fixed root's own reader (whose
// names are its own) and is newer_form at a variable one; form 2 is
// message_form_as_file; any other byte is newer_form.
func formName(b byte, fixed bool) string {
	switch {
	case b == 1, b == 3 && fixed:
		return ""
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

// sealDescriptor writes `integrity = "crc32c"` into the emitted descriptor of
// root, the fact §2.14's table-line key declares. The emitter does not carry
// the key into the descriptor yet (luautable.go is outside this card), so the
// test lays it in the way load_test.go's injectKeyVs lays in the key variants.
func sealDescriptor(t *testing.T, g *generatedUnit, root string) {
	t.Helper()
	anchor := fmt.Sprintf("_T.%sDescriptor = _schema_tables.table({\n\tname = %q,\n", root, root)
	for name, data := range g.files {
		if !strings.Contains(string(data), anchor) {
			continue
		}
		g.files[name] = []byte(strings.Replace(string(data), anchor, anchor+"\tintegrity = \"crc32c\",\n", 1))
		if err := os.WriteFile(filepath.Join(g.dir, name), g.files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no emitted module declares %sDescriptor", root)
}

// idTable is a form-1 file's id table: the ids, then their count (§3).
func idTable(ids ...uint64) []byte {
	var b []byte
	for _, x := range append(ids, uint64(len(ids))) {
		for i := 0; i < 8; i++ {
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
	// the first field the walk saw, pinned where the schema says it (the wire
	// files are named for it); every other counter row must name one
	pinnedFields := map[string]string{
		"parts_elem_kind":         "parts",
		"parts_count_past_bound":  "parts",
		"trace_elem_kind":         "trace",
		"trace_count_past_bound":  "trace",
		"a2_arm_length_off_width": "b",
	}
	compared := 0
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

	// a table that declares the trailer, with the same field, so Feed's wire
	// is its wire (§2.14)
	seal := generateFiles(t, map[string]string{"Seal.schema": "package seal\n\ntable Ledger | integrity = \"crc32c\"\n{\n    id uint32\n}\n"})
	sealDescriptor(t, seal, "Ledger")
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
		refusalCase{key: "variable_early_end", unit: "streamdemo", root: "Feed", input: "early_end", form: -1, want: "wire_damaged", field: "-"},
		// a variable root: form 2 is a message, form 3 and anything else newer
		refusalCase{key: "variable_message", unit: "streamdemo", root: "Feed", input: "empty", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "variable_fixed", unit: "streamdemo", root: "Feed", input: "empty", form: 3, want: "newer_form", field: "-"},
		refusalCase{key: "variable_unknown", unit: "streamdemo", root: "Feed", input: "empty", form: 127, want: "newer_form", field: "-"},
		// a fixed root: form 1 is READ (a one-byte file is the framing's
		// damage), form 2 a message, an unknown byte newer
		refusalCase{key: "fixed_variable_form", unit: fixedUnit, root: fixedRoot, input: "empty", form: 1, want: "wire_damaged", field: "-"},
		refusalCase{key: "fixed_message", unit: fixedUnit, root: fixedRoot, input: "empty", form: 2, want: "message_form_as_file", field: "-"},
		refusalCase{key: "fixed_newer", unit: fixedUnit, root: fixedRoot, input: "empty", form: 9, want: "newer_form", field: "-"},
		// a form-3 byte at a fixed root goes to its own fixed reader, which
		// names a header too short to hold a layout (§3.4)
		refusalCase{key: "fixed_own_reader", unit: fixedUnit, root: fixedRoot, input: "empty", form: 3, want: "layout_malformed", field: "-"},
	)
	// a declared fixed root with no Luau fixed layout: form 3 is not carried
	// by this call, unknown_form, and its form 1 is read (a one-byte file is
	// the framing's damage). A record past §3.4's 65536-byte ceiling is one
	// by construction: the fixed form is not emitted for it and the class is
	// kept; the corpus's own, when it has one, is a case too
	big := generateFiles(t, map[string]string{"Big.schema": "package big\n\nfixed table Big\n{\n    a [70000]uint8\n}\n"})
	gens["big"], roots["big"] = big, emittedRoots(big)
	if er := roots["big"]["Big"]; !er.fixed || er.layout {
		t.Fatalf("Big is not a declared fixed root without a fixed layout: %+v", er)
	}
	cases = append(cases,
		refusalCase{key: "bare_fixed_form3", unit: "big", root: "Big", input: "empty", form: 3, want: "unknown_form", field: "-"},
		refusalCase{key: "bare_fixed_form1", unit: "big", root: "Big", input: "empty", form: 1, want: "wire_damaged", field: "-"},
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
		"message_form_as_file", "newer_form", "unknown_form", "integrity_mismatch", "layout_malformed", "ok"} {
		if names[name] == 0 {
			t.Errorf("no case answered %s", name)
		}
	}
	t.Logf("%d report rows compared, %d cases in all; names %v; fixed root %s.%s, bare fixed root %q", compared, len(cases), names, fixedUnit, fixedRoot, bareRoot)
}
