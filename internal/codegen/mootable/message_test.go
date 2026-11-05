package mootable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageRow is one `message` row of the tables manifest, with the
// `connection` it names (testdata/conformance/tables/FORMAT.md).
type messageRow struct {
	name, unit, root, fileWire, messageWire string
	connWire, buildVersion                  string
}

func messageRows(t *testing.T) []messageRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type conn struct{ unit, version, wire string }
	conns := map[string]conn{}
	var raw [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "connection":
			conns[fields[1]] = conn{fields[2], fields[3], fields[4]}
		case "message":
			if fields[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
				raw = append(raw, fields)
			}
		}
	}
	var out []messageRow
	for _, r := range raw {
		c, ok := conns[r[2]]
		if !ok {
			t.Fatalf("message %s names no connection %s", r[1], r[2])
		}
		out = append(out, messageRow{name: r[1], unit: c.unit, root: r[3], fileWire: r[4], messageWire: r[5], connWire: c.wire, buildVersion: c.version})
	}
	return out
}

func readRoot(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// messageProbe is the moo main for one row: it prints the unit's own
// announcement and build version, then (1) AnnounceRead the connection,
// LoadMessages the batch, MeasureMessages and SaveMessages it, and saves the
// first body in the variable form; (2) ReadStrict the file-form wire and
// SaveMessages it as a batch of one. Each answer is one hex line or a named
// refusal.
func messageProbe(g *generatedUnit, mod, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal save = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + fmt.Sprintf("local T = require(\"./%s\")\n", mod) + fmt.Sprintf(`local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
print("ann " .. hex(T.TableMessageAnnouncement))
print("ver " .. hex(buffer.fromstring(T.TableMessageBuildVersion)))
local vocabulary = MS.NewVocabulary()
local report: any = {}
if not MS.AnnounceRead(vocabulary, inputs.conn, report) then print("ANNOUNCE " .. tostring(report.reason)) return end
local n, values = MS.LoadMessages(vocabulary, %q, inputs.message, 256, report)
print(string.format("load %%d %%d,%%d,%%d,%%d,%%d,%%s", n, report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
if n < 1 then print("LOAD " .. tostring(report.reason)) return end
local size = MS.MeasureMessages(%q, values, report)
local out = MS.SaveMessages(%q, values, report)
if out == nil then print("SAVE " .. tostring(report.reason) .. " " .. tostring(report.field)) return end
print("msg " .. tostring(size) .. " " .. hex(out))
local file, why = save.save(St.lookup(%q), values[1])
if file == nil then print("FILESAVE " .. tostring(why and why.name)) return end
print("file " .. hex(file))
local rep2 = load.newReport()
local v = load.ReadStrict(%q, inputs.file, rep2)
if v == nil then print("READ " .. tostring(rep2.reason)) return end
local again = MS.SaveMessages(%q, { v }, report)
if again == nil then print("SAVE1 " .. tostring(report.reason) .. " " .. tostring(report.field)) return end
print("one " .. hex(again))
`, rootName, rootName, rootName, rootName, rootName, rootName)
}

// tableModuleOf names the <Base>Table module that declares root.
func tableModuleOf(g *generatedUnit, rootName string) string {
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			if st.Name == rootName {
				return f.Base + "Table"
			}
		}
	}
	return ""
}

func parseProbe(out string) map[string]string {
	m := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		k, v, _ := strings.Cut(line, " ")
		m[k] = v
	}
	return m
}

// TestMooMessageFormMatchesTheManifest is the message form's conformance gate
// (docs/SPEC-TABLES.md §3.3; the manifest's `connection` and `message` rows):
// for every row, the unit's own announcement equals the pinned connection
// bytes and its build version the row's column; the message-form batch,
// announced through the connection, loads clean with the Go engine's counters
// (tablewire.DecodeMessages over the same bytes) and re-saves byte for byte
// with its measure equal to its length; its body re-saves as the pinned
// file-form wire; and the file-form wire, read strictly, saves as the pinned
// message — the two encodings of one value, both ways.
func TestMooMessageFormMatchesTheManifest(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := messageRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no message row")
	}
	cache := map[string]*generatedUnit{}
	compared := 0
	for _, row := range rows {
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			t.Errorf("message %s: unit %s is refused: %v", row.name, row.unit, g.err)
			continue
		}
		if st := g.unit.Tables[row.root]; st == nil || !st.FixedDeclared {
			continue // a variable table carries no message form (§3.3)
		}
		conn := readRoot(t, row.connWire)
		msg := readRoot(t, row.messageWire)
		file := readRoot(t, row.fileWire)
		out := parseProbe(g.run(t, messageProbe(g, tableModuleOf(g, row.root), row.root), map[string][]byte{"conn": conn, "message": msg, "file": file}))
		if out["ann"] != hex.EncodeToString(conn) {
			t.Errorf("message %s: the unit's announcement is not the pinned connection %s", row.name, row.connWire)
		}
		v, _ := strconv.ParseUint(strings.TrimPrefix(row.buildVersion, "0x"), 16, 64)
		if want := hex.EncodeToString(u64le(v)); out["ver"] != want {
			t.Errorf("message %s: build version %s, the row says %s", row.name, out["ver"], row.buildVersion)
		}
		// the Go engine's counters over the same batch
		m := tabletext.NewModel(g.unit)
		var vocab tablewire.Vocabulary
		var grep tabletext.Report
		if err := vocab.AnnounceRead(conn, &grep); err != nil {
			t.Fatalf("message %s: the Go engine refuses the announcement: %v", row.name, err)
		}
		insts := make([]*tabletext.Instance, 256)
		for i := range insts {
			insts[i] = m.New(g.unit.Tables[row.root])
		}
		var drep tabletext.Report
		n, _, err := tablewire.DecodeMessages(m, insts, msg, &vocab, &drep)
		if err != nil {
			t.Fatalf("message %s: the Go engine refuses the batch: %v", row.name, err)
		}
		want := fmt.Sprintf("%d %d,%d,%d,%d,%d,%v", n, drep.Unknown, 0, drep.Widened, 0, 0, drep.Refused)
		if out["load"] != want {
			t.Errorf("message %s: moo load %q, the Go engine %q", row.name, out["load"], want)
			continue
		}
		if wantMsg := fmt.Sprintf("%d %x", len(msg), msg); out["msg"] != wantMsg {
			t.Errorf("message %s: the batch does not re-save byte for byte:\nmoo: %s\nwire: %s\n(probe: %v)", row.name, out["msg"], wantMsg, out)
			continue
		}
		if out["file"] != hex.EncodeToString(file) {
			t.Errorf("message %s: the loaded body does not save as the pinned file form:\nmoo:  %s\nfile: %x", row.name, out["file"], file)
			continue
		}
		if out["one"] != hex.EncodeToString(msg) {
			t.Errorf("message %s: the file form does not save as the pinned message:\nmoo: %s\nmsg: %x", row.name, out["one"], msg)
			continue
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no message row compared")
	}
	t.Logf("%d of %d message rows compared both ways", compared, len(rows))
}

// TestMooMessageFormNegativeControl patches the emitted table_message.moo so
// the batch count rides one less than it is, and requires the re-save
// comparison to go red on the first manifest row.
func TestMooMessageFormNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	row := messageRows(t)[0]
	g := generateUnit(t, units, row.unit)
	if g.err != nil {
		t.Fatal(g.err)
	}
	path := filepath.Join(g.dir, "table_message.moo")
	src := string(g.files["table_message.moo"])
	const anchor = "\tput(c.w, n - 1, 8)\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the count anchor is not in table_message.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "\tput(c.w, n, 8)\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := readRoot(t, row.messageWire)
	out := parseProbe(g.run(t, messageProbe(g, tableModuleOf(g, row.root), row.root), map[string][]byte{"conn": readRoot(t, row.connWire), "message": msg, "file": readRoot(t, row.fileWire)}))
	if out["msg"] == fmt.Sprintf("%d %x", len(msg), msg) {
		t.Fatalf("the patched count still re-saves the pinned batch: %v", out)
	}
}

// messageDiffProbe writes one value's message form both ways: the Go
// engine's file wire read strictly and saved as a batch of one, and the Go
// engine's message batch, announced through the unit's own announcement,
// loaded and saved in the variable form.
func messageDiffProbe(g *generatedUnit, mod, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal save = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + fmt.Sprintf("local T = require(\"./%s\")\n", mod) + fmt.Sprintf(`local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local report: any = {}
local v = load.ReadStrict(%q, inputs.file, load.newReport())
if v == nil then print("READ") return end
local out = MS.SaveMessages(%q, { v }, report)
if out == nil then print("SAVE " .. tostring(report.reason) .. " " .. tostring(report.field)) return end
print("one " .. hex(out))
local vocabulary = MS.NewVocabulary()
if not MS.AnnounceRead(vocabulary, T.TableMessageAnnouncement, report) then print("ANNOUNCE " .. tostring(report.reason)) return end
local n, values = MS.LoadMessages(vocabulary, %q, inputs.message, 256, report)
print(string.format("load %%d %%d,%%d,%%d,%%d,%%d,%%s", n, report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
if n ~= 1 then return end
local file = save.save(St.lookup(%q), values[1])
if file == nil then print("FILESAVE") return end
print("file " .. hex(file))
`, rootName, rootName, rootName, rootName)
}

// messageFixedCases restate, in the fixed class, what the message form
// carries of the constructCases the fixed class refuses as written (§2.2,
// §3.3): a list of tables held at a bound instead of an unbounded array root,
// an optional table whose text is bounded instead of a blob, and unions,
// keyed arrays and enum lists at a bound instead of beside a map. Text lists,
// maps, pointers and blobs have no fixed restatement: they are the variable
// class's alone.
var messageFixedCases = []constructCase{
	{"bounded_table_lists", "§2.9 bounded arrays of tables", "package probe\ntable Goal\n{\n    x int32\n    tag string(4)\n}\ntable GoalList\n{\n    goals [..4]Goal\n    pair [2]Goal\n    none [..3]Goal\n}\n",
		"GoalList", `{"goals":[{"x":1,"tag":"a"},{"x":-2}],"pair":[{"x":3},{"x":4,"tag":"dd"}],"none":[]}`},
	{"optional_fixed", "§2.7 optional tables", "package probe\ntable Inner\n{\n    text string(8)\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n    gone ?Inner\n    tail int32\n}\n",
		"Outer", `{"inner":{"text":"hi","n":4},"tail":5}`},
	{"unions_keyed_enums", "§2.6 §3.2 unions, keyed arrays, enums", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks [..4]Pick\n    trio [3]Pick\n    slots [..3]Slot\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"trio":[{"label":"abcdefgh"},{},{"count":4294967295}],"slots":["feet","head"],"flags_ok":true}`},
}

// TestMooMessageFormMatchesTheGoEngine is the message form's differential
// against the Go reference engine over every construct the variable wire's
// gate carries (construct_test.go's cases) and the fixed restatements above:
// the Go engine writes the value's file form (tablewire.Encode) and its
// message form (tablewire.EncodeMessages); moo saves the file form as the
// same message, and loads the message clean into the same file form. THE
// MESSAGE FORM IS FIXED TABLES ONLY (§3.3): each construct is declared fixed,
// and the compiler's fixed-class refusal (§2.2, §11) is the one reason a
// construct is not compared; every other construct is.
func TestMooMessageFormMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	cases := append(append([]constructCase(nil), constructCases...), messageFixedCases...)
	compared, variable := 0, 0
	for _, c := range cases {
		switch variableClass, problem := messageDiffers(t, c, nil); {
		case variableClass:
			t.Logf("%s: a variable-class construct, refused fixed: no message form", c.name)
			variable++
		case problem != "":
			t.Errorf("%s: %s", c.name, problem)
		default:
			compared++
		}
	}
	if compared == 0 || compared+variable != len(cases) {
		t.Fatalf("%d of %d constructs compared, %d refused fixed: every fixed-class construct is compared", compared, len(cases), variable)
	}
	t.Logf("%d of %d constructs compared both ways; the other %d are the variable class's (no message form)", compared, len(cases), variable)
}

// messageDiffers runs one construct's differential, the emitted sources
// first passed through patch when it is not nil. It answers whether the
// compiler refuses the construct in the fixed class (and so it has no message
// form), and otherwise what disagrees with the Go engine, or "".
func messageDiffers(t *testing.T, c constructCase, patch func(files map[string][]byte)) (variableClass bool, problem string) {
	t.Helper()
	u, err := compiler.New().Load([]string{writeSchema(t, fixedTables.ReplaceAllString(c.schema, "fixed table "))})
	if err != nil {
		if !fixedClassRefusal.MatchString(err.Error()) {
			return false, fmt.Sprintf("the fixed declaration is refused for a reason other than the fixed class: %v", err)
		}
		return true, ""
	}
	var vocab tablewire.Vocabulary
	var rep tabletext.Report
	if err := vocab.AnnounceRead(ir.TableAnnouncement(u), &rep); err != nil || !vocab.Announced() {
		return false, fmt.Sprintf("the Go engine refuses the unit's own announcement (%v %+v)", err, rep)
	}
	// a sealed root's trailer (§2.14, §3.3) is the Go engine's own, on both
	// forms: Encode and EncodeMessages write it, Decode and DecodeMessages
	// check it
	file := goOracleWire(t, u, c.root, c.json)
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[c.root])
	var r2 tabletext.Report
	if ok, err := tablewire.Decode(m, inst, file, &r2); !ok || err != nil {
		t.Fatalf("%s: the Go engine does not read its own wire: %v", c.name, err)
	}
	msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		return false, fmt.Sprintf("the Go engine writes no message form for a fixed root: %v", err)
	}
	// what the message form carries back: the Go engine's own read of its
	// batch, in the file form (a message elides an empty string or bytes
	// whatever its default, so the trip is not always the original value)
	back := m.New(u.Tables[c.root])
	var r3 tabletext.Report
	if _, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{back}, msg, &vocab, &r3); err != nil {
		t.Fatalf("%s: the Go engine does not read its own batch: %v", c.name, err)
	}
	backFile, err := tablewire.Encode(m, back)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	g := writeUnit(t, u, files)
	out := parseProbe(g.run(t, messageDiffProbe(g, tableModuleOf(g, c.root), c.root), map[string][]byte{"file": file, "message": msg}))
	if out["one"] != hex.EncodeToString(msg) {
		return false, fmt.Sprintf("moo's message is not the Go engine's:\nmoo: %v\ngo:  %x", out, msg)
	}
	if out["load"] != "1 0,0,0,0,0,false" || out["file"] != hex.EncodeToString(backFile) {
		return false, fmt.Sprintf("moo does not load the Go engine's message into the Go engine's file form:\nmoo:  %v\nfile: %x", out, backFile)
	}
	return false, ""
}

// TestMooMessageFormFixedCasesNegativeControl plants, for each fixed
// restatement, one fault in the emitted table_message.moo's writer on the path
// that case alone was added to reach, and requires that case's differential
// to go red: an absent optional table written and a present one elided, a
// counted list's last element dropped, and a keyed array's present count one
// short.
func TestMooMessageFormFixedCasesNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	plants := map[string][2]string{
		"optional_fixed":      {"\t\tif not v[fd.present] then\n\t\t\treturn true\n", "\t\tif v[fd.present] then\n\t\t\treturn true\n"},
		"bounded_table_lists": {"\t\t\twref(c, entry.slot)\n\t\t\treturn encodeArray(c, fd, entry, v, val, n)\n", "\t\t\twref(c, entry.slot)\n\t\t\treturn encodeArray(c, fd, entry, v, val, n - 1)\n"},
		"unions_keyed_enums":  {"\tput(c.w, #present, bitsReq(0, shape.max))\n", "\tput(c.w, #present - 1, bitsReq(0, shape.max))\n"},
	}
	if len(plants) != len(messageFixedCases) {
		t.Fatalf("%d plants for %d fixed restatements", len(plants), len(messageFixedCases))
	}
	for _, c := range messageFixedCases {
		plant, ok := plants[c.name]
		if !ok {
			t.Fatalf("%s: no plant", c.name)
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			variableClass, problem := messageDiffers(t, c, func(files map[string][]byte) {
				src := string(files["table_message.moo"])
				if strings.Count(src, plant[0]) != 1 {
					t.Fatalf("the %s anchor is not in table_message.moo exactly once", c.name)
				}
				files["table_message.moo"] = []byte(strings.Replace(src, plant[0], plant[1], 1))
			})
			if variableClass || problem == "" {
				t.Fatalf("the planted fault still agrees with the Go engine (variable class %v)", variableClass)
			}
			t.Logf("red as required: %s", strings.SplitN(problem, "\n", 2)[0])
		})
	}
}

// quantSchema is a compressed float on two ranges (docs/SPEC-TABLES.md §3.3,
// the quantizer table's own two shapes).
const quantSchema = "package probe\nfixed table Q\n{\n    a float32 | min = 0, max = 1, resolution = 0.01\n    b float32 | min = -1, max = 1, resolution = 0.01\n}\n"

// TestMooMessageQuantizerIsTheGoEngines holds the writer's quantizer to
// ir.TableMessageQuantize over the special values the quantizer table names
// (-0, +0, below min, above max) and values around every step boundary of a
// sweep, by comparing moo's message bytes with the Go engine's
// (tablewire.EncodeMessages) for each value; and holds the finite rule: a NaN
// and both infinities are REFUSED on write as non_finite_float, never clamped.
func TestMooMessageQuantizerIsTheGoEngines(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	u := unitFromSource(t, quantSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	var values []float32
	values = append(values, float32(math.Copysign(0, -1)), 0, -2, 2, -1, 1, 0.5, 0.005, 0.015, 0.995, 1e-30, -1e-30, 3e38, -3e38)
	for i := 0; i <= 200; i += 7 {
		x := float32(i) / 200
		values = append(values, x, math.Nextafter32(x, -1), math.Nextafter32(x, 2))
	}
	m := tabletext.NewModel(u)
	var main strings.Builder
	main.WriteString("--!strict\nlocal MS = require(\"./table_message\")\nlocal T = require(\"./ProbeTable\")\n")
	main.WriteString(`local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function one(a: number, b: number)
	local v = T.NewQ()
	v.A = a
	v.B = b
	local report: any = {}
	local out = MS.SaveMessages("Q", { v }, report)
	if out == nil then print("REFUSED " .. tostring(report.reason) .. " " .. tostring(report.field)) else print(hex(out)) end
end
`)
	var want []string
	for _, x := range values {
		inst := m.New(u.Tables["Q"])
		inst.Fields[0].Cell.F = float64(x)
		inst.Fields[1].Cell.F = float64(x)
		msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("the Go engine refuses %v: %v", x, err)
		}
		want = append(want, hex.EncodeToString(msg))
		fmt.Fprintf(&main, "one(0x%08x, 0x%08x)\n", math.Float32bits(x), math.Float32bits(x))
	}
	for _, bits := range []uint32{0x7FC00000, 0x7F800001, 0x7F800000, 0xFF800000} {
		fmt.Fprintf(&main, "one(0x%08x, 0)\n", bits)
		want = append(want, "REFUSED non_finite_float Q.a")
	}
	got := strings.Split(g.run(t, main.String(), nil), "\n")
	if len(got) != len(want) {
		t.Fatalf("%d answers for %d values", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			label := "a non-finite value"
			if i < len(values) {
				label = fmt.Sprintf("%v (0x%08x)", values[i], math.Float32bits(values[i]))
			}
			t.Errorf("%s: moo %s, want %s", label, got[i], want[i])
		}
	}
}

// TestMooMessageQuantizerNegativeControl patches the quantizer's rounding
// half out (floor instead of round) and requires the comparison to go red.
func TestMooMessageQuantizerNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	u := unitFromSource(t, quantSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "Fl.floorToInt(Fl.fadd(scaled, F_HALF))"
	src := string(files["table_message.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the rounding anchor is not in table_message.moo exactly once")
	}
	files["table_message.moo"] = []byte(strings.Replace(src, anchor, "Fl.floorToInt(scaled)", 1))
	g := writeUnit(t, u, files)
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables["Q"])
	inst.Fields[0].Cell.F = float64(float32(0.996))
	msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	out := g.run(t, fmt.Sprintf("--!strict\nlocal MS = require(\"./table_message\")\nlocal T = require(\"./ProbeTable\")\nlocal v = T.NewQ()\nv.A = 0x%08x\nlocal out = MS.SaveMessages(\"Q\", { v }, {})\nif out == nil then print(\"nil\") return end\nlocal p = {}\nfor i = 0, buffer.len(out) - 1 do p[#p + 1] = string.format(\"%%02x\", buffer.readu8(out, i)) end\nprint(table.concat(p))\n", math.Float32bits(float32(0.996))), nil)
	if out == hex.EncodeToString(msg) {
		t.Fatalf("the patched quantizer still writes the Go engine's index: %s", out)
	}
}

// evolveCase is one value written by build A and read by build B, which
// declares the same table differently (docs/SPEC-TABLES.md §3.3, §4: the
// reader decodes at the SENDER's announced shape and lands the value in its
// own declaration).
type evolveCase struct {
	name, writer, reader, root, json string
}

var evolveCases = []evolveCase{
	{"widened_unknown_mismatch",
		"package probe\nfixed table T\n{\n    a int16\n    gone uint8\n    b int32\n    c float32\n    d uint16 | min = 0, max = 1000\n}\n",
		"package probe\nfixed table T\n{\n    a int32\n    b string(8)\n    c float64\n    d uint16 | min = 0, max = 100\n}\n",
		"T", `{"a":-300,"gone":7,"b":5,"c":1.5,"d":500}`},
	{"quantized_into_plain_and_clamped",
		"package probe\nfixed table T\n{\n    q float32 | min = -10, max = 10, resolution = 0.5\n    r float32 | min = 0, max = 1, resolution = 0.01\n}\n",
		"package probe\nfixed table T\n{\n    q float32 | min = -1, max = 1, resolution = 0.5\n    r float32\n}\n",
		"T", `{"q":7.5,"r":0.25}`},
	{"arrays_unions_enums",
		"package probe\nenum E { a, b, c }\nunion U\n{\n    n uint32\n    s string(8)\n    extra uint8\n}\nfixed table T\n{\n    xs [..8]int16\n    u U\n    v U\n    e E\n    keyed [E]uint8\n}\n",
		"package probe\nenum E { a, b }\nunion U\n{\n    n uint64\n    s string(4)\n}\nfixed table T\n{\n    xs [..4]int32\n    u U\n    v U\n    e E\n    keyed [E]uint8\n}\n",
		"T", `{"xs":[1,-2,3,4,5,6],"u":{"extra":9},"v":{"s":"abcdefgh"},"e":"c","keyed":{"a":1,"c":3}}`},
	{"nested",
		"package probe\nfixed table In\n{\n    k int8\n    w uint8\n}\nfixed table T\n{\n    inner In\n    pair [2]In\n}\n",
		"package probe\nfixed table In\n{\n    k int16\n}\nfixed table T\n{\n    inner In\n    pair [2]In\n}\n",
		"T", `{"inner":{"k":-5,"w":2},"pair":[{"k":1},{"w":4}]}`},
}

// TestMooMessageFormEvolvesAsTheGoEngine is the reader's evolution gate on
// form 2: build A writes a value (the Go engine's EncodeMessages) and build B
// reads it against A's announcement. moo running build B and the Go engine
// reading into build B's model agree on the counters (unknown, kind_mismatch,
// widened, clamped, duplicate, malformed) and on the value, compared in B's
// file form.
func TestMooMessageFormEvolvesAsTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	for _, c := range evolveCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ua := unitFromSource(t, c.writer)
			ub := unitFromSource(t, c.reader)
			ma := tabletext.NewModel(ua)
			ia := ma.New(ua.Tables[c.root])
			var ra tabletext.Report
			if !ma.Read(ia, []byte(c.json), &ra) || ra.Refused {
				t.Fatalf("the Go engine does not read the writer's JSON: %+v", ra)
			}
			msg, err := tablewire.EncodeMessages(ma, []*tabletext.Instance{ia})
			if err != nil {
				t.Fatal(err)
			}
			ann := ir.TableAnnouncement(ua)
			var vocab tablewire.Vocabulary
			var rv tabletext.Report
			if err := vocab.AnnounceRead(ann, &rv); err != nil {
				t.Fatal(err)
			}
			mb := tabletext.NewModel(ub)
			ib := mb.New(ub.Tables[c.root])
			var rb tabletext.Report
			n, _, err := tablewire.DecodeMessages(mb, []*tabletext.Instance{ib}, msg, &vocab, &rb)
			if err != nil {
				t.Fatal(err)
			}
			fileB, err := tablewire.Encode(mb, ib)
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("load %d %d,%d,%d,%d,%d,%v\nfile %x", n, rb.Unknown, 0, rb.Widened, 0, 0, rb.Refused, fileB)
			files, err := generate(ub)
			if err != nil {
				t.Fatal(err)
			}
			if got := runEvolve(t, ub, files, c.root, ann, msg); got != want {
				t.Fatalf("build B reading build A's message:\nmoo: %s\ngo:  %s", got, want)
			}
			t.Logf("%s", strings.SplitN(want, "\n", 2)[0])
		})
	}
}

func runEvolve(t *testing.T, ub *ir.Unit, files map[string][]byte, rootName string, ann, msg []byte) string {
	t.Helper()
	g := writeUnit(t, ub, files)
	c := struct{ root string }{rootName}
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal save = require(\"./table_save\")\nlocal MS = require(\"./table_message\")\nlocal St = require(\"./schema_tables\")\n" + g.requireAll() +
		fmt.Sprintf(`local vocabulary = MS.NewVocabulary()
local report: any = {}
if not MS.AnnounceRead(vocabulary, inputs.ann, report) then print("ANNOUNCE " .. tostring(report.reason)) return end
local n, values = MS.LoadMessages(vocabulary, %q, inputs.message, 256, report)
print(string.format("load %%d %%d,%%d,%%d,%%d,%%d,%%s", n, report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed)))
local out = save.save(St.lookup(%q), values[1])
if out == nil then print("FILESAVE") return end
local p = {}
for i = 0, buffer.len(out) - 1 do p[#p + 1] = string.format("%%02x", buffer.readu8(out, i)) end
print("file " .. table.concat(p))
`, c.root, c.root)
	return g.run(t, main, map[string][]byte{"ann": ann, "message": msg})
}

// TestMooMessageFormEvolvesNegativeControl patches the reader's body walk so
// a field announced at a foreign kind is not counted kind_mismatch, and
// requires the first evolution case to go red on exactly that counter.
func TestMooMessageFormEvolvesNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	c := evolveCases[0]
	ua := unitFromSource(t, c.writer)
	ub := unitFromSource(t, c.reader)
	ma := tabletext.NewModel(ua)
	ia := ma.New(ua.Tables[c.root])
	var ra tabletext.Report
	if !ma.Read(ia, []byte(c.json), &ra) {
		t.Fatal("the writer's JSON does not read")
	}
	msg, err := tablewire.EncodeMessages(ma, []*tabletext.Instance{ia})
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(ub)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "\t\t\tt.report.kindMismatch += 1\n\t\t\tif not skipEntry(t, entry.kind, entry.shape) then\n\t\t\t\treturn dmal(t)\n"
	src := string(files["table_message.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the body's kind-mismatch anchor is not in table_message.moo exactly once")
	}
	files["table_message.moo"] = []byte(strings.Replace(src, anchor, strings.Replace(anchor, "+= 1", "+= 0", 1), 1))
	got := runEvolve(t, ub, files, c.root, ir.TableAnnouncement(ua), msg)
	if !strings.HasPrefix(got, "load 1 1,0,2,1,0,false") {
		t.Fatalf("the patched reader did not drop exactly the kind mismatch: %s", got)
	}
}

// fixedTables finds every `table` declaration of a schema source, to declare it
// `fixed table` instead.
var fixedTables = regexp.MustCompile(`(?m)^table `)

// fixedClassRefusal is the compiler's refusal of a variable-class construct
// in a fixed table (docs/SPEC-TABLES.md §2.2, §11), whose first line names
// `fixed` as what to drop.
var fixedClassRefusal = regexp.MustCompile(`(?s)^[^\n]*(drop .fixed.|VARIABLE-table)`)

// writeSchema writes one schema source to a fresh file and answers its path.
func writeSchema(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
