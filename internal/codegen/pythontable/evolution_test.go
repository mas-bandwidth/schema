package pythontable

// THE EVOLUTION CONSTRUCTS on the Python table leg (ROADMAP.md, Variable
// tables: "string, bytes and flags defaults, and renaming a table" and
// "renaming variants, arms and type fields"; docs/SPEC-TABLES.md §3, §4, §5,
// §2.15).
//
// Every case is held to the Go engine. The first set is the engine's own
// form-1 wire of a JSON text, written by an OLD declaration and read by the
// generated Python package of the NEW one, then written back: a value at a
// declared string, bytes or flags default is elided as a scalar is, so the
// empty wire is the canonical wire of an all-default table; a value off its
// default rides and must come back byte for byte; a table, a table field and
// an enum variant renamed under `was` keep the old name's hash.
//
// The second set is the other direction and the refusals: a NEWER wire read
// by an OLD declaration (fields, nested-table fields and a `*T` the old
// reader does not declare, skipped and counted unknown), and hand-built wires
// the engine refuses, each with the exact reason the engine names. SKIPPED IS
// NOT UNREAD (§4): a field the reader skips is walked by the same wire rules,
// so ill-formed text, a bad element kind, a reference past the id table or a
// bad pointer inside it refuses the load. For every wire the engine's verdict
// (the reason it refuses, or its unknown count and its canonical re-encoding
// of what it read) is the Python leg's, under both pinned interpreters.
//
// The third set is the write side: a value outside its declaration (not a
// str, a string or bytes over its bound, an enum ordinal that names no
// variant, a deprecated variant) raises InvalidValue naming the field, writes
// nothing and leaves the caller's object as it was; the engine's own writer
// refuses the same values.
//
// The closure is union-free (a union arm's identity is the unions card's), and
// the declared kinds are this card's own (string, bytes, flags, an enum's
// variant) and the base's (bool, int, by-value tables, a single pointer); a
// skipped payload may hold any kind the wire carries.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtEvolutionCase is one Go-engine construct: schema (the NEW side, the unit
// the Python package is generated from), oldSchema (the writer's unit, empty
// when it is the same declaration), root and the JSON text the engine writes.
// check is the probe lines that hold the loaded value.
type vtEvolutionCase struct {
	name      string
	oldSchema string
	schema    string
	root      string
	json      string
	check     string
}

const vtDefaultsSchema = `package probe

flags Caps { Jump, Crouch, Fly }

table D
{
    s    string(8) = "hi"
    b    bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    n    int32     = 5 | min = 0, max = 9
}
`

const vtRenameOld = `package probe

flags Caps { Jump, Crouch, Fly }

table Vessel
{
    name string(32) = "untitled"
    tag  bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    hull int32     = 100 | min = 0, max = 1000
}

table Fleet
{
    title    string(16) = "fleet"
    flagship *Vessel
    home     Vessel
}
`

const vtRenameNew = `package probe

flags Caps { Jump, Crouch, Fly }

table Ship | was = "Vessel"
{
    name string(32) = "untitled"
    tag  bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    hull int32     = 100 | min = 0, max = 1000
}

table Fleet
{
    title    string(16) = "fleet"
    flagship *Ship
    home     Ship
}
`

const vtRenameFieldOld = `package probe

table Record
{
    past int32 | min = 0, max = 100
}
`

const vtRenameFieldNew = `package probe

table Record
{
    moved int32 | min = 0, max = 100, was = "past"
}
`

// vtRenameFieldUnrenamed is vtRenameFieldNew without its `was`: the id the
// rename control plants is this declaration's own.
const vtRenameFieldUnrenamed = `package probe

table Record
{
    moved int32 | min = 0, max = 100
}
`

const vtRenameVariantOld = `package probe

enum Grade { Bronze, Silver, Gold }

table Cfg
{
    grade Grade = Bronze
    tally int32 = 3 | min = 0, max = 9
}
`

const vtRenameVariantNew = `package probe

enum Grade
{
    Bronze
    Argent | was = "Silver"
    Gold
}

table Cfg
{
    grade Grade = Bronze
    tally int32 = 3 | min = 0, max = 9
}
`

var vtEvolutionCases = []vtEvolutionCase{
	{"defaults_at", "", vtDefaultsSchema, "D", `{"s":"hi","b":"YWI=","caps":["Jump","Fly"],"n":5}`,
		`print("defaults", value.s == "hi", value.b == b"ab", value.caps == 5, value.n == 5)`},
	{"defaults_off", "", vtDefaultsSchema, "D", `{"s":"","b":"","caps":[],"n":0}`,
		`print("off", value.s == "", value.b == b"", value.caps == 0, value.n == 0)`},
	{"rename_table", vtRenameOld, vtRenameNew, "Fleet",
		`{"title":"armada","flagship":{"name":"Aurora","tag":"enpx","caps":["Crouch"],"hull":250},"home":{}}`,
		`print("renamed", value.title, value.flagship.name, value.home.name, value.flagship.caps, value.home.caps)`},
	{"rename_field", vtRenameFieldOld, vtRenameFieldNew, "Record", `{"past":7}`,
		`print("field", value.moved)`},
	{"rename_variant", vtRenameVariantOld, vtRenameVariantNew, "Cfg", `{"grade":"Silver","tally":7}`,
		`print("variant", value.grade, Cfg().grade, value.tally)`},
}

// vtEngineWire is the Go engine's form-1 wire of the JSON text at root.
func vtEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

// vtEvolutionProbe is the program the pinned interpreters run: load the wire,
// save it back, print the verdict and then the case's own value checks.
func vtEvolutionProbe(root, check string) string {
	return pyrun.ImportPrelude + `from probe import ` + root + `

with open("wire", "rb") as fh:
    data = fh.read()
try:
    value = ` + root + `.load(data)
except Exception as err:
    print("load_refused", type(err).__name__, getattr(err, "reason", ""))
    raise SystemExit(0)
else:
    out = value.save()
    print("roundtrip", len(out), out == data, value.measure() == len(out))
` + check + `
`
}

// vtRunBoth runs one probe under both pinned interpreters and answers the
// current pin's stdout; the floor must print the same bytes.
func vtRunBoth(t *testing.T, name, dir string, inputs map[string][]byte) string {
	t.Helper()
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != floor {
		t.Fatalf("%s: the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", name, got, floor)
	}
	return got
}

// vtGenerate writes the generator's tree for schema (edited by mutate when
// set) into a fresh directory.
func vtGenerate(t *testing.T, name, schema string, mutate func(*testing.T, map[string][]byte)) string {
	t.Helper()
	u := loadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: the Python backend refuses it: %v", name, err)
	}
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	return dir
}

// vtEvolutionRun writes the generator's tree (edited by mutate when set),
// writes the engine's wire and answers the probe's stdout.
func vtEvolutionRun(t *testing.T, c vtEvolutionCase, mutate func(*testing.T, map[string][]byte)) string {
	t.Helper()
	wire := vtEvolutionWire(t, c)
	dir := vtGenerate(t, c.name, c.schema, mutate)
	probe := vtEvolutionProbe(c.root, c.check)
	return vtRunBoth(t, c.name, dir, map[string][]byte{"main.py": []byte(probe), "wire": wire})
}

// vtEvolutionFailures is every line of a case's probe that did not hold.
func vtEvolutionFailures(t *testing.T, c vtEvolutionCase, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	got := vtEvolutionRun(t, c, mutate)
	var fails []string
	wantWire := fmt.Sprintf("roundtrip %d True True", len(vtEvolutionWire(t, c)))
	if !strings.Contains(got, wantWire+"\n") {
		fails = append(fails, fmt.Sprintf("%s: the save is not the wire, want %q:\n%s", c.name, wantWire, got))
	}
	for _, line := range vtEvolutionWant[c.name] {
		if !strings.Contains(got, line+"\n") {
			fails = append(fails, fmt.Sprintf("%s: the probe lacks %q:\n%s", c.name, line, got))
		}
	}
	return fails
}

// vtEvolutionWire is the engine wire the case is held to.
func vtEvolutionWire(t *testing.T, c vtEvolutionCase) []byte {
	t.Helper()
	writerSchema := c.schema
	if c.oldSchema != "" {
		writerSchema = c.oldSchema
	}
	writer := loadUnit(t, writerSchema)
	return vtEngineWire(t, writer, c.root, c.json)
}

var vtEvolutionWant = map[string][]string{
	"defaults_at":    {"defaults True True True True"},
	"defaults_off":   {"off True True True True"},
	"rename_table":   {"renamed armada Aurora untitled 2 5"},
	"rename_field":   {"field 7"},
	"rename_variant": {"variant 2 1 7"},
}

func vtCase(t *testing.T, name string) vtEvolutionCase {
	t.Helper()
	for _, c := range vtEvolutionCases {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("the %s construct is absent", name)
	return vtEvolutionCase{}
}

// THE OLD READER AND THE NEWER WRITER. The newer Doc adds string, bytes, enum
// and flags fields, a by-value table field and a `*Extra` to a table the old
// reader does not declare; the newer Inner adds a string. Flat is a value-only
// table in the old reader (the straight-line shape) whose newer writer points
// at an Extra.

const vtSkipNew = `package probe

flags Caps { Jump, Crouch, Fly }

enum Grade { Bronze, Silver, Gold }

table Extra
{
    label string(8)
    data  bytes(4)
}

table Inner
{
    v    int32 | min = 0, max = 99
    note string(8)
}

table Doc
{
    id    int32 = 7 | min = 0, max = 99
    inner Inner
    next  *Inner
    s     string(8) = "hi"
    b     bytes(4)  = "ab"
    g     Grade     = Gold
    caps  Caps      = { Jump }
    more  Inner
    link  *Extra
}

table Flat
{
    id   int32 | min = 0, max = 99
    link *Extra
}
`

const vtSkipOld = `package probe

table Inner
{
    v int32 | min = 0, max = 99
}

table Doc
{
    id    int32 = 7 | min = 0, max = 99
    inner Inner
    next  *Inner
}

table Flat
{
    id int32 | min = 0, max = 99
}
`

// THE DEPRECATED VARIANT (§2.15): the writer's Silver is the reader's
// deprecated slot, and the field's declared default is not zero.

const vtDeprecatedWriter = `package probe

enum Grade { Bronze, Silver, Gold }

table Pick
{
    g     Grade = Gold
    tally int32 = 3 | min = 0, max = 9
}
`

const vtDeprecatedReader = `package probe

enum Grade
{
    Bronze
    Silver | deprecated
    Gold
}

table Pick
{
    g     Grade = Gold
    tally int32 = 3 | min = 0, max = 9
}
`

// vtWire is a hand-built form-1 file: the root body's fields, its
// terminator, the id table and its count.
func vtLeb(v uint64) []byte {
	return binary.AppendUvarint(nil, v)
}

func vtCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// vtField is one field: its reference, its kind byte and its payload.
func vtField(ref uint64, kind byte, payload ...[]byte) []byte {
	return vtCat(vtLeb(ref), []byte{kind}, vtCat(payload...))
}

// vtFramed is a payload behind its length.
func vtFramed(b []byte) []byte { return vtCat(vtLeb(uint64(len(b))), b) }

// vtBody is a table body: its fields and its terminator.
func vtBody(fields ...[]byte) []byte { return vtCat(vtCat(fields...), []byte{0}) }

// vtRecord is one node record: its type reference, its length and its body.
func vtRecord(typeRef uint64, body []byte) []byte { return vtCat(vtLeb(typeRef), vtFramed(body)) }

func vtFile(ids []uint64, fields ...[]byte) []byte {
	out := vtCat([]byte{ir.TableWireForm}, vtBody(fields...))
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// The id table every hand-built wire carries: an id no declaration here
// names (ref 1), the node table's (ref 2), a type id no declaration here
// names (ref 3), and Doc.next's and Doc.s's own ids (refs 4 and 5), read off
// the unit the case is read with.
const (
	vtRefUnknown = 1
	vtRefNode    = 2
	vtRefType    = 3
	vtRefNext    = 4
	vtRefS       = 5
	vtRefB       = 6
	vtRefG       = 7
)

func vtIds(t *testing.T, u *ir.Unit) []uint64 {
	t.Helper()
	id := func(table, field string) uint64 {
		st := u.Tables[table]
		if st == nil {
			t.Fatalf("no table %s", table)
		}
		for _, f := range st.Fields {
			if f.Name == field {
				return ir.TableFieldWireId(f)
			}
		}
		// a field this unit does not declare: the hash of a name nothing here
		// spells, so the reference still resolves
		return ir.TableWireId("probe.absent." + table + "." + field)
	}
	return []uint64{
		ir.TableWireId("probe.unknown.field"),
		ir.TableNodeWireId,
		ir.TableWireId("probe.unknown.Type"),
		id("Doc", "next"),
		id("Doc", "s"),
		id("Doc", "b"),
		id("Doc", "g"),
	}
}

// vtNested is n table payloads, each inside the last, as one skipped field.
func vtNested(n int) []byte {
	body := vtBody()
	for range n - 1 {
		body = vtBody(vtField(vtRefUnknown, ir.TableKindTable, vtFramed(body)))
	}
	return vtField(vtRefUnknown, ir.TableKindTable, vtFramed(body))
}

// vtWireCase is one wire read by the generated package of reader at root:
// either the engine's own wire of json written under writer, or a
// hand-built wire. want is the engine's verdict, held here by name: the
// reason it refuses, or "" where it accepts.
type vtWireCase struct {
	name   string
	reader string
	root   string
	writer string
	json   string
	wire   func(ids []uint64) []byte
	want   string
}

func vtBad(name, root string, want string, wire func(ids []uint64) []byte) vtWireCase {
	return vtWireCase{name: name, reader: vtSkipOld, root: root, wire: wire, want: want}
}

func vtKnownBad(name, want string, wire func(ids []uint64) []byte) vtWireCase {
	return vtWireCase{name: name, reader: vtSkipNew, root: "Doc", wire: wire, want: want}
}

var (
	vtNaN64   = binary.LittleEndian.AppendUint64(nil, math.Float64bits(math.NaN()))
	vtIllText = []byte{0xff, 0xfe}
)

var vtWireCases = []vtWireCase{
	// A NEWER WIRE, READ BY THE OLD DECLARATION: every field, nested field and
	// node record the old reader cannot name is skipped and counted.
	{name: "old_reads_new_doc", reader: vtSkipOld, root: "Doc", writer: vtSkipNew,
		json: `{"id":3,"inner":{"v":4,"note":"in"},"next":{"v":5,"note":"nx"},"s":"hello","b":"AQID","g":"Silver","caps":["Fly"],"more":{"v":6,"note":"mo"},"link":{"label":"ok","data":"AA=="}}`},
	{name: "old_reads_new_defaults", reader: vtSkipOld, root: "Doc", writer: vtSkipNew, json: `{}`},
	{name: "old_reads_new_flat", reader: vtSkipOld, root: "Flat", writer: vtSkipNew,
		json: `{"id":2,"link":{"label":"x","data":"AQ=="}}`},
	{name: "deprecated_reads_default", reader: vtDeprecatedReader, root: "Pick", writer: vtDeprecatedWriter,
		json: `{"g":"Silver","tally":4}`},
	{name: "deprecated_neighbour_reads", reader: vtDeprecatedReader, root: "Pick", writer: vtDeprecatedWriter,
		json: `{"g":"Bronze","tally":4}`},

	// SKIPPED IS NOT UNREAD: the old reader refuses what the engine refuses
	// inside a field it does not declare, for every kind a skipped field can
	// hold.
	vtBad("skip_text_ill_formed", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindString, vtFramed(vtIllText)))
	}),
	vtBad("skip_text_nul", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindString, vtFramed([]byte("a\x00"))))
	}),
	vtBad("skip_text_ill_formed_flat", "Flat", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindString, vtFramed(vtIllText)))
	}),
	vtBad("skip_enum_past_ids", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindEnum, vtLeb(uint64(len(ids)+1))))
	}),
	vtBad("skip_bytes_element_kind", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindArray, vtFramed([]byte{ir.TableKindRetiredWstring, 0})))
	}),
	vtBad("skip_bytes_trailing", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindArray, vtFramed([]byte{ir.TableKindU8, 1, 'A', 'B'})))
	}),
	vtBad("skip_list_ill_formed_text", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindArray,
			vtFramed(vtCat([]byte{ir.TableKindString, 1}, vtFramed(vtIllText)))))
	}),
	vtBad("skip_list_count_cap", "Doc", "count_over_extent_cap", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindArray,
			vtFramed(vtCat([]byte{ir.TableKindU8}, vtLeb(1<<31)))))
	}),
	vtBad("skip_table_ill_formed_text", "Doc", "wire_damaged", func(ids []uint64) []byte {
		inner := vtBody(vtField(vtRefUnknown, ir.TableKindString, vtFramed(vtIllText)))
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindTable, vtFramed(inner)))
	}),
	vtBad("skip_table_bool_byte", "Doc", "wire_damaged", func(ids []uint64) []byte {
		inner := vtBody(vtField(vtRefUnknown, ir.TableKindBool, []byte{2}))
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindTable, vtFramed(inner)))
	}),
	vtBad("skip_map_ill_formed_text", "Doc", "wire_damaged", func(ids []uint64) []byte {
		entry := vtBody(vtField(vtRefUnknown, ir.TableKindString, vtFramed(vtIllText)))
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindArray,
			vtFramed(vtCat([]byte{ir.TableKindTable, 1}, vtFramed(entry)))))
	}),
	vtBad("skip_keyed_zero_key", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindKeyed,
			vtFramed(vtCat([]byte{ir.TableKindU8, 1, 0}, vtFramed([]byte{7})))))
	}),
	vtBad("skip_union_ill_formed_text", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindUnion,
			vtLeb(vtRefUnknown), []byte{ir.TableKindString}, vtFramed(vtIllText)))
	}),
	vtBad("skip_float_non_finite", "Doc", "non_finite_float", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindF64, vtNaN64))
	}),
	vtBad("skip_kind_no_reader_skips", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindRetiredWstring, vtFramed(nil)))
	}),
	vtBad("skip_field_twice", "Doc", "wire_damaged", func(ids []uint64) []byte {
		f := vtField(vtRefUnknown, ir.TableKindU8, []byte{1})
		return vtFile(ids, f, f)
	}),
	vtBad("skip_nested_past_bound", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtNested(ir.TableBinaryMaxDepth+1))
	}),
	vtBad("skip_nested_at_bound", "Doc", "", func(ids []uint64) []byte {
		return vtFile(ids, vtNested(ir.TableBinaryMaxDepth))
	}),
	vtBad("skip_pointer_past_nodes", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(2)))
	}),
	vtBad("skip_pointer_to_root", "Doc", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(ir.NodeWireIndexRoot)))
	}),
	vtBad("skip_record_ill_formed_text", "Doc", "wire_damaged", func(ids []uint64) []byte {
		rec := vtRecord(vtRefType, vtBody(vtField(vtRefUnknown, ir.TableKindString, vtFramed(vtIllText))))
		return vtFile(ids,
			vtField(vtRefNode, ir.TableKindString, vtFramed(vtCat(vtLeb(1), rec))),
			vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(2)))
	}),
	vtBad("skip_record_unreached", "Doc", "wire_damaged", func(ids []uint64) []byte {
		rec := vtRecord(vtRefType, vtBody())
		return vtFile(ids, vtField(vtRefNode, ir.TableKindString, vtFramed(vtCat(vtLeb(1), rec))))
	}),
	vtBad("skip_record_cycle", "Doc", "wire_damaged", func(ids []uint64) []byte {
		rec := vtRecord(vtRefType, vtBody(vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(2))))
		return vtFile(ids,
			vtField(vtRefNode, ir.TableKindString, vtFramed(vtCat(vtLeb(1), rec))),
			vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(2)))
	}),
	vtBad("skip_pointer_to_unknown_record", "Doc", "", func(ids []uint64) []byte {
		rec := vtRecord(vtRefType, vtBody(vtField(vtRefUnknown, ir.TableKindString, vtFramed([]byte("ok")))))
		return vtFile(ids,
			vtField(vtRefNode, ir.TableKindString, vtFramed(vtCat(vtLeb(1), rec))),
			vtField(vtRefUnknown, ir.TableKindPointer, vtLeb(2)))
	}),
	vtBad("known_pointer_to_unknown_record", "Doc", "", func(ids []uint64) []byte {
		rec := vtRecord(vtRefType, vtBody())
		return vtFile(ids,
			vtField(vtRefNode, ir.TableKindString, vtFramed(vtCat(vtLeb(1), rec))),
			vtField(vtRefNext, ir.TableKindPointer, vtLeb(2)))
	}),

	// THE NEW READER'S OWN FIELDS: a known string, bytes or enum the engine
	// refuses.
	vtKnownBad("known_string_over_bound", "length_over_bound", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefS, ir.TableKindString, vtFramed([]byte("123456789"))))
	}),
	vtKnownBad("known_text_ill_formed", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefS, ir.TableKindString, vtFramed(vtIllText)))
	}),
	vtKnownBad("known_bytes_element_not_u8", "kind_mismatch", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefB, ir.TableKindArray, vtFramed([]byte{ir.TableKindU16, 0})))
	}),
	vtKnownBad("known_bytes_over_bound", "length_over_bound", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefB, ir.TableKindArray, vtFramed([]byte{ir.TableKindU8, 5, 1, 2, 3, 4, 5})))
	}),
	vtKnownBad("known_enum_past_ids", "wire_damaged", func(ids []uint64) []byte {
		return vtFile(ids, vtField(vtRefG, ir.TableKindEnum, vtLeb(uint64(len(ids)+1))))
	}),
}

// vtWireOf is the case's wire: the engine's write of its JSON, or the
// hand-built bytes over the reader unit's ids.
func vtWireOf(t *testing.T, c vtWireCase, reader *ir.Unit) []byte {
	t.Helper()
	if c.wire != nil {
		return c.wire(vtIds(t, reader))
	}
	return vtEngineWire(t, loadUnit(t, c.writer), c.root, c.json)
}

// vtEngineLine is the engine's verdict on the case's wire as the probe prints
// it: the reason it refuses, or its unknown count and the hex of its own
// canonical write of the value it read.
func vtEngineLine(t *testing.T, c vtWireCase, reader *ir.Unit, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(reader)
	st := reader.Tables[c.root]
	if st == nil {
		t.Fatalf("%s: no table %s", c.name, c.root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if !ok {
		reason := tablewire.RefusalReason(err)
		if reason != c.want {
			t.Fatalf("%s: the engine refuses with %q (%v), the case says %q", c.name, reason, err, c.want)
		}
		return fmt.Sprintf("case %s refused %s", c.name, reason)
	}
	if c.want != "" {
		t.Fatalf("%s: the engine accepts the wire, the case says it refuses with %q", c.name, c.want)
	}
	canonical, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: the engine does not write back what it read: %v", c.name, err)
	}
	return fmt.Sprintf("case %s accepted unknown=%d save=%s measure=True", c.name, rep.Unknown, hex.EncodeToString(canonical))
}

const vtWireProbe = `
from probe import _schema

for name, root in CASES:
    with open("w_" + name, "rb") as fh:
        data = fh.read()
    report = _schema.LoadReport()
    try:
        value = root.load(data, report)
    except _schema.Refused as err:
        print("case", name, "refused", err.reason)
        continue
    out = value.save()
    print("case", name, "accepted", "unknown=" + str(report.unknown), "save=" + out.hex(), "measure=" + str(value.measure() == len(out)))
`

// vtWireGroups is the wire cases by reader schema, in case order.
func vtWireGroups(cases []vtWireCase) (order []string, groups map[string][]vtWireCase) {
	groups = map[string][]vtWireCase{}
	for _, c := range cases {
		if _, ok := groups[c.reader]; !ok {
			order = append(order, c.reader)
		}
		groups[c.reader] = append(groups[c.reader], c)
	}
	return order, groups
}

// vtWireFailures runs one reader schema's wire cases in one probe under both
// pins and answers every case whose Python line is not the engine's.
func vtWireFailures(t *testing.T, reader string, cases []vtWireCase, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	u := loadUnit(t, reader)
	inputs := map[string][]byte{}
	want := map[string]string{}
	roots := map[string]bool{}
	var list strings.Builder
	list.WriteString("CASES = [\n")
	for _, c := range cases {
		wire := vtWireOf(t, c, u)
		inputs["w_"+c.name] = wire
		want[c.name] = vtEngineLine(t, c, u, wire)
		roots[c.root] = true
		fmt.Fprintf(&list, "    (%q, %s),\n", c.name, c.root)
	}
	list.WriteString("]\n")
	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	probe := pyrun.ImportPrelude + "from probe import " + strings.Join(names, ", ") + "\n" + list.String() + vtWireProbe
	inputs["main.py"] = []byte(probe)
	dir := vtGenerate(t, cases[0].name, reader, mutate)
	got := vtRunBoth(t, cases[0].name, dir, inputs)
	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "case" {
			lines[fields[1]] = line
		}
	}
	var fails []string
	for _, c := range cases {
		if lines[c.name] != want[c.name] {
			fails = append(fails, fmt.Sprintf("%s: Python answers %q, the engine %q", c.name, lines[c.name], want[c.name]))
		}
	}
	return fails
}

// vtWriteCase is one value outside its declaration: the probe sets field of a
// fresh root to bad (a Python expression) and the save must raise
// InvalidValue at path, write nothing and leave the object as it was. engine,
// when set, plants the same value in the engine's instance, whose own writer
// must refuse it.
type vtWriteCase struct {
	name   string
	schema string
	root   string
	field  string
	bad    string
	path   string
	engine func(*tabletext.Cell)
}

var vtWriteCases = []vtWriteCase{
	{"write_string_not_str", vtSkipNew, "Doc", "s", "5", "s", nil},
	{"write_string_over_bound", vtSkipNew, "Doc", "s", `"123456789"`, "s",
		func(c *tabletext.Cell) { c.Str = []byte("123456789") }},
	{"write_string_ill_formed", vtSkipNew, "Doc", "s", `"a\x00b"`, "s",
		func(c *tabletext.Cell) { c.Str = []byte("a\x00b") }},
	{"write_bytes_not_bytes", vtSkipNew, "Doc", "b", `"ab"`, "b", nil},
	{"write_bytes_over_bound", vtSkipNew, "Doc", "b", `b"12345"`, "b",
		func(c *tabletext.Cell) { c.Str = []byte("12345") }},
	{"write_enum_no_variant", vtSkipNew, "Doc", "g", "4", "g",
		func(c *tabletext.Cell) { c.U = 4 }},
	{"write_enum_deprecated", vtDeprecatedReader, "Pick", "g", "2", "g",
		func(c *tabletext.Cell) { c.U = 2 }},
}

const vtWriteProbe = `
from probe import _schema

for name, root, field, bad in CASES:
    value = root()
    setattr(value, field, bad)
    before = repr(value)
    for verb in ("save", "measure"):
        try:
            out = getattr(value, verb)()
        except _schema.InvalidValue as err:
            print("write", name, verb, "refused", err.path, repr(value) == before)
        else:
            print("write", name, verb, "accepted", out)
`

func vtWriteFailures(t *testing.T, schema string, cases []vtWriteCase) []string {
	t.Helper()
	u := loadUnit(t, schema)
	m := tabletext.NewModel(u)
	var fails []string
	var list strings.Builder
	list.WriteString("CASES = [\n")
	roots := map[string]bool{}
	for _, c := range cases {
		fmt.Fprintf(&list, "    (%q, %s, %q, %s),\n", c.name, c.root, c.field, c.bad)
		roots[c.root] = true
		if c.engine == nil {
			continue
		}
		inst := m.New(u.Tables[c.root])
		planted := false
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == c.field {
				c.engine(&inst.Fields[i].Cell)
				planted = true
			}
		}
		if !planted {
			t.Fatalf("%s: no field %s.%s", c.name, c.root, c.field)
		}
		if _, err := tablewire.Encode(m, inst); err == nil {
			fails = append(fails, fmt.Sprintf("%s: the engine's writer accepts the value the case says it refuses", c.name))
		}
	}
	list.WriteString("]\n")
	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	probe := pyrun.ImportPrelude + "from probe import " + strings.Join(names, ", ") + "\n" + list.String() + vtWriteProbe
	dir := vtGenerate(t, cases[0].name, schema, nil)
	got := vtRunBoth(t, cases[0].name, dir, map[string][]byte{"main.py": []byte(probe)})
	for _, c := range cases {
		for _, verb := range []string{"save", "measure"} {
			line := fmt.Sprintf("write %s %s refused %s True", c.name, verb, c.path)
			if !strings.Contains(got, line+"\n") {
				fails = append(fails, fmt.Sprintf("%s: the probe lacks %q:\n%s", c.name, line, got))
			}
		}
	}
	return fails
}

// Functional (the pinned interpreters and analyzer): run with SCHEMA_SLOW=1.
func TestPythonVtEvolution(t *testing.T) {
	t.Parallel()
	for _, c := range vtEvolutionCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, f := range vtEvolutionFailures(t, c, nil) {
				t.Error(f)
			}
			u := loadUnit(t, c.schema)
			dir := vtGenerate(t, c.name, c.schema, nil)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("%s: mypy --strict over the generated package:\n%s", c.name, out)
			}
			if out, err := pyrun.Analyze(t, dir, u.Package+"/_schema"); err != nil {
				t.Fatalf("%s: mypy --strict over the runtime:\n%s", c.name, out)
			}
		})
	}
	order, groups := vtWireGroups(vtWireCases)
	for i, reader := range order {
		cases := groups[reader]
		t.Run(fmt.Sprintf("wire_%d_%s", i, cases[0].root), func(t *testing.T) {
			t.Parallel()
			for _, f := range vtWireFailures(t, reader, cases, nil) {
				t.Error(f)
			}
			dir := vtGenerate(t, cases[0].name, reader, nil)
			if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
				t.Fatalf("%s: mypy --strict over the generated package:\n%s", cases[0].name, out)
			}
		})
	}
	writeSchemas := []string{}
	writeGroups := map[string][]vtWriteCase{}
	for _, c := range vtWriteCases {
		if _, ok := writeGroups[c.schema]; !ok {
			writeSchemas = append(writeSchemas, c.schema)
		}
		writeGroups[c.schema] = append(writeGroups[c.schema], c)
	}
	for i, schema := range writeSchemas {
		cases := writeGroups[schema]
		t.Run(fmt.Sprintf("write_%d_%s", i, cases[0].root), func(t *testing.T) {
			t.Parallel()
			for _, f := range vtWriteFailures(t, schema, cases) {
				t.Error(f)
			}
		})
	}
}

// vtReplaceOnce replaces the one occurrence of anchor in the generated file
// at path, failing t when the anchor does not ride exactly once.
func vtReplaceOnce(t *testing.T, files map[string][]byte, path, anchor, with string) {
	t.Helper()
	text := string(files[path])
	if n := strings.Count(text, anchor); n != 1 {
		t.Fatalf("the negative control's anchor %q rides %d times in %s, want 1", anchor, n, path)
	}
	files[path] = []byte(strings.Replace(text, anchor, with, 1))
}

// vtReplaceInPackage is vtReplaceOnce over the one generated (non-runtime)
// module that holds anchor.
func vtReplaceInPackage(t *testing.T, files map[string][]byte, anchor, with string) {
	t.Helper()
	var hits []string
	for path, data := range files {
		if strings.Contains(path, "/_schema/") {
			continue
		}
		if strings.Contains(string(data), anchor) {
			hits = append(hits, path)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("the negative control's anchor %q rides in %d generated modules (%v), want 1", anchor, len(hits), hits)
	}
	vtReplaceOnce(t, files, hits[0], anchor, with)
}

// vtControlRed requires a control's failures to be non-empty and the first to
// name the construct the control plants its fault in.
func vtControlRed(t *testing.T, what, name string, fails []string) {
	t.Helper()
	if len(fails) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s left the gate green", what)
	}
	if !strings.HasPrefix(fails[0], name+":") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not by the name %s:\n%s", name, fails[0])
	}
}

// TestPythonVtEvolutionNegativeControl plants a defect in the runtime's string
// write, in a copy of the generated package, and requires the rename_table
// construct (whose nodes carry strings off their defaults) to go red: a gate is
// only watching the codec if a broken codec fails it.
//
// Functional (the pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionNegativeControl(t *testing.T) {
	t.Parallel()
	mutate := func(t *testing.T, files map[string][]byte) {
		vtReplaceOnce(t, files, "probe/_schema/kind_string.py", "w.raw(encoded)", "w.raw(encoded[:-1])")
	}
	vtControlRed(t, "a truncated string write", "rename_table", vtEvolutionFailures(t, vtCase(t, "rename_table"), mutate))
}

// TestPythonVtEvolutionNegativeControlDefaults plants a wrong declared default
// in a copy of the generated D (n is declared = 5; the copy is born at 6), and
// requires defaults_at, whose wire omits every field, to go red: absent must
// read the DECLARED default, and the gate watches it.
//
// Functional (the pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionNegativeControlDefaults(t *testing.T) {
	t.Parallel()
	mutate := func(t *testing.T, files map[string][]byte) {
		vtReplaceInPackage(t, files, "self.n = 5\n", "self.n = 6\n")
	}
	vtControlRed(t, "a wrong declared default", "defaults_at", vtEvolutionFailures(t, vtCase(t, "defaults_at"), mutate))
}

// TestPythonVtEvolutionNegativeControlRenames replaces the renamed field's
// wire id (the hash of its `was` name) with the hash of its declared name, in
// a copy of the generated Record, and requires rename_field to go red: a
// renamed field keeps its wire id, and the gate watches it.
//
// Functional (the pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionNegativeControlRenames(t *testing.T) {
	t.Parallel()
	fieldID := func(schema string) uint64 {
		u := loadUnit(t, schema)
		return ir.TableFieldWireId(u.Tables["Record"].Fields[0])
	}
	renamed, declared := fieldID(vtRenameFieldNew), fieldID(vtRenameFieldUnrenamed)
	if renamed == declared {
		t.Fatal("the renamed field's id is its declared name's: the control would plant nothing")
	}
	mutate := func(t *testing.T, files map[string][]byte) {
		vtReplaceInPackage(t, files, "field_id="+hex64(renamed)+",", "field_id="+hex64(declared)+",")
	}
	vtControlRed(t, "a renamed field under its declared name's id", "rename_field", vtEvolutionFailures(t, vtCase(t, "rename_field"), mutate))
}

// TestPythonVtEvolutionNegativeControlSkipCheck removes the text check from
// the runtime's skip walk, in a copy of the generated package, and requires
// skip_text_ill_formed (ill-formed text in a field the old reader skips) to
// go red: a skipped value is checked, and the gate watches it.
//
// Functional (the pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionNegativeControlSkipCheck(t *testing.T) {
	t.Parallel()
	mutate := func(t *testing.T, files map[string][]byte) {
		vtReplaceOnce(t, files, "probe/_schema/load.py", `_check_text(data, "ill-formed text")`, "pass")
	}
	var cases []vtWireCase
	for _, c := range vtWireCases {
		if c.name == "skip_text_ill_formed" {
			cases = append(cases, c)
		}
	}
	if len(cases) != 1 {
		t.Fatal("the skip_text_ill_formed construct is absent")
	}
	vtControlRed(t, "a skip walk without its text check", "skip_text_ill_formed", vtWireFailures(t, cases[0].reader, cases, mutate))
}
