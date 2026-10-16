package jstable

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// viewDump is what view_test's probe prints of one generated <Home>View.js:
// the registry and every descriptor, the reference columns spelled by the
// NAME they reach (a descriptor graph has cycles, a dump cannot).
type viewDump struct {
	Package    string                   `json:"package"`
	ProtocolId string                   `json:"protocolId"`
	Tables     []string                 `json:"tables"`
	Types      []string                 `json:"types"`
	Desc       map[string]viewDumpType  `json:"descriptors"`
	Vocab      map[string]viewDumpVocab `json:"vocabularies"`
	Consts     map[string]viewDumpConst `json:"constants"`
}

type viewDumpType struct {
	What   string          `json:"what"`
	File   string          `json:"file"`
	Size   int64           `json:"size"`
	Doc    string          `json:"doc"`
	Tags   []string        `json:"tags"`
	Fields []viewDumpField `json:"fields"`
}

type viewDumpField struct {
	Name          string   `json:"name"`
	Json          *string  `json:"json"`
	Id            string   `json:"id"`
	TypeName      string   `json:"typeName"`
	Kind          int      `json:"kind"`
	Offset        int64    `json:"offset"`
	ElementSize   int64    `json:"elementSize"`
	ArrayBound    int64    `json:"arrayBound"`
	CountOffset   int64    `json:"countOffset"`
	PresentOffset int64    `json:"presentOffset"`
	Vocabulary    string   `json:"vocabulary"`
	Key           string   `json:"key"`
	Table         string   `json:"table"`
	Doc           string   `json:"doc"`
	Tags          []string `json:"tags"`
}

type viewDumpVocab struct {
	Kind          string            `json:"kind"`
	Max           int64             `json:"max"`
	PayloadOffset int64             `json:"payloadOffset"`
	Doc           string            `json:"doc"`
	Tags          []string          `json:"tags"`
	Variants      []viewDumpVariant `json:"variants"`
}

type viewDumpVariant struct {
	Value   int64          `json:"value"`
	Name    string         `json:"name"`
	Id      string         `json:"id"`
	Payload string         `json:"payload"`
	Field   *viewDumpField `json:"field"`
	Doc     string         `json:"doc"`
	Tags    []string       `json:"tags"`
}

type viewDumpConst struct {
	TypeName string   `json:"typeName"`
	IsFloat  bool     `json:"isFloat"`
	Int      string   `json:"intValue"`
	Doc      string   `json:"doc"`
	Tags     []string `json:"tags"`
}

const viewProbe = `
import { pathToFileURL } from "node:url";
const { UnitView, TableDocNone, TableTagsNone } = await import(pathToFileURL(process.argv[2]).href);
const v = UnitView();
if (v !== UnitView()) { throw new Error("UnitView() is not one registry"); }
const name = (r) => (r === null ? "" : r.name);
const field = (f) => ({ name: f.name, json: f.json, id: f.id.toString(16), typeName: f.typeName, kind: f.kind,
  offset: f.offset, elementSize: f.elementSize, arrayBound: f.arrayBound, countOffset: f.countOffset,
  presentOffset: f.presentOffset, vocabulary: name(f.vocabulary), key: name(f.key), table: name(f.table),
  doc: f.doc, tags: [...f.tags] });
for (const t of Object.values(v.descriptors)) {
  for (const f of t.fields) {
    if (typeof f.doc !== "string" || !Array.isArray(f.tags)) { throw new Error(t.name + "." + f.name + ": a doc or tags column is missing"); }
    if (f.doc === "" && f.doc !== TableDocNone) { throw new Error("unreachable"); }
    if (f.tags.length === 0 && f.tags !== TableTagsNone) { throw new Error(t.name + "." + f.name + ": empty tags are not the one shared list"); }
    if (!Object.isFrozen(f)) { throw new Error(t.name + "." + f.name + ": a descriptor is not frozen"); }
  }
}
const vocab = {};
for (const set of [v.enums, v.flags, v.unions]) {
  for (const r of set) {
    vocab[r.name] = { kind: r.kind, max: r.max, payloadOffset: r.payloadOffset ?? -1, doc: r.doc, tags: [...r.tags],
      variants: r.variants.map((x) => ({ value: x.value, name: x.name, id: x.id.toString(16),
        payload: x.payload === undefined || x.payload === null ? "" : x.payload.name,
        field: x.field === undefined || x.field === null ? null : field(x.field), doc: x.doc, tags: [...x.tags] })) };
  }
}
const descriptors = {};
for (const [n, t] of Object.entries(v.descriptors)) {
  descriptors[n] = { what: t.what, file: t.file, size: t.size, doc: t.doc, tags: [...t.tags], fields: t.fields.map(field) };
}
const constants = {};
for (const c of v.constants) {
  constants[c.name] = { typeName: c.typeName, isFloat: c.isFloat, intValue: c.intValue.toString(), doc: c.doc, tags: [...c.tags] };
}
process.stdout.write(JSON.stringify({ package: v.package, protocolId: v.protocolId.toString(16),
  tables: v.tables.map((r) => r.name), types: v.types.map((r) => r.name), descriptors, vocabularies: vocab, constants }));
`

// viewRun generates one corpus unit's view module, runs the probe over it
// under node and decodes the dump.
func viewRun(t *testing.T, node, dir string) (*ir.Unit, viewDump) {
	t.Helper()
	u := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "tables", dir)})
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	work := t.TempDir()
	module := filepath.Join(work, viewBase(u)+".js")
	src, ok := files[viewBase(u)+".js"]
	if !ok {
		t.Fatalf("%s: the JavaScript leg emitted no view module %s.js (docs/SPEC-TABLES.md §8.5)", dir, viewBase(u))
	}
	if err := os.WriteFile(module, src, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(viewProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, module)
	cmd.Dir = work
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("%s: the view probe did not run: %v\n%s", dir, err, stderr)
	}
	var d viewDump
	if err := json.Unmarshal(out, &d); err != nil {
		t.Fatalf("%s: the probe's dump does not decode: %v", dir, err)
	}
	return u, d
}

func viewTagsEqual(got, want []string) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	return reflect.DeepEqual(got, want)
}

// TestUnitViewDescriptorsJavaScript holds the JavaScript view
// (docs/SPEC-TABLES.md §8) to the IR over two corpus units, tables/examples and
// tables/arms, running the GENERATED descriptors under node:
//
//   - every table AND every type has a descriptor (§8.1, §8.2), and the
//     registry lists the tables and the types in declaration-name order with a
//     map's generated entry table not a row (§8.3);
//   - every field's OFFSET and ELEMENT SIZE are the blittable record's
//     (ir.RecordLayout, ir.BlockFieldOf; §7.2, §19.3): inside the record, in
//     declaration order, an array's slots within the field's storage;
//   - every declaration, field, variant, arm and constant carries the doc and
//     tags the IR read (SPEC.md §4.1, §4.2), an absent doc the one shared
//     TableDocNone and an absent tag list the one shared TableTagsNone;
//   - the VOCABULARY — the enum Grade's variants, the union Effect's and the
//     union Carry's arms — is written out HERE, names, docs and tags, rather
//     than derived from the descriptors, and each id is the wire id (§5) of
//     the variant's own name;
//   - an enum field names its enum, a union field its union, a nested table
//     its descriptor; a record-naming arm names its payload descriptor and a
//     general arm carries a field descriptor at the union's payload base.
func TestUnitViewDescriptorsJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	for _, dir := range []string{"examples", "arms"} {
		u, d := viewRun(t, node, dir)
		if d.Package != u.Package || d.ProtocolId != fmt.Sprintf("%x", u.ProtocolId) {
			t.Errorf("%s: the registry says package %s protocol %s; the unit is %s %x", dir, d.Package, d.ProtocolId, u.Package, u.ProtocolId)
		}
		types, tables := viewDeclarations(u)
		var wantTables, wantTypes []string
		for _, st := range tables {
			if st.MapEntryOf == "" {
				wantTables = append(wantTables, st.Name)
			}
		}
		for _, st := range types {
			wantTypes = append(wantTypes, st.Name)
		}
		if !reflect.DeepEqual(d.Tables, wantTables) || !viewTagsEqual(d.Types, wantTypes) {
			t.Errorf("%s: the registry lists tables %v and types %v; the unit declares %v and %v", dir, d.Tables, d.Types, wantTables, wantTypes)
		}
		closure := ir.TableClosure(u)
		for _, st := range append(append([]*ir.Struct{}, types...), tables...) {
			desc, ok := d.Desc[st.Name]
			if !ok {
				t.Errorf("%s: %s has no descriptor (docs/SPEC-TABLES.md §8.1, §8.2)", dir, st.Name)
				continue
			}
			if desc.Doc != st.Doc || !viewTagsEqual(desc.Tags, st.Tags) {
				t.Errorf("%s: %s carries doc %q tags %v; the IR has %q %v", dir, st.Name, desc.Doc, desc.Tags, st.Doc, st.Tags)
			}
			ml := ir.RecordLayout(u, st)
			if desc.Size != ml.Size || len(desc.Fields) != len(st.Fields) {
				t.Errorf("%s: %s is %d bytes with %d fields; the layout is %d bytes with %d", dir, st.Name, desc.Size, len(desc.Fields), ml.Size, len(st.Fields))
				continue
			}
			last := int64(-1)
			for i, f := range st.Fields {
				g := desc.Fields[i]
				fl := ml.FieldByName(f.Name)
				if g.Name != f.Name || fl == nil {
					t.Errorf("%s: %s field %d is %q; the IR declares %q", dir, st.Name, i, g.Name, f.Name)
					continue
				}
				facts := ir.BlockFieldOf(u, f, fl.Offset, false)
				elem := facts.ElemSize
				if elem == 0 {
					elem = fl.Size
				}
				if g.Offset != fl.Offset || g.ElementSize != elem || g.CountOffset != facts.CountOffset || g.PresentOffset != facts.PresentOffset {
					t.Errorf("%s: %s.%s sits at %d (element %d, count %d, present %d); the record has it at %d (element %d, count %d, present %d)",
						dir, st.Name, f.Name, g.Offset, g.ElementSize, g.CountOffset, g.PresentOffset, fl.Offset, elem, facts.CountOffset, facts.PresentOffset)
				}
				if g.Offset <= last || g.Offset+fl.Size > ml.Size {
					t.Errorf("%s: %s.%s at %d is not inside the record past the field before it (%d, record %d)", dir, st.Name, f.Name, g.Offset, last, ml.Size)
				}
				last = g.Offset
				if g.ArrayBound > 0 && f.Array != ir.ArrayNone && !f.Type.Pointer && g.ElementSize*g.ArrayBound > fl.Size {
					t.Errorf("%s: %s.%s holds %d slots of %d bytes in %d", dir, st.Name, f.Name, g.ArrayBound, g.ElementSize, fl.Size)
				}
				if g.Doc != f.Doc || !viewTagsEqual(g.Tags, f.Tags) {
					t.Errorf("%s: %s.%s carries doc %q tags %v; the IR has %q %v", dir, st.Name, f.Name, g.Doc, g.Tags, f.Doc, f.Tags)
				}
				if g.TypeName != ir.TableTypeSpelling(f) {
					t.Errorf("%s: %s.%s names type %q; the IR spells %q", dir, st.Name, f.Name, g.TypeName, ir.TableTypeSpelling(f))
				}
				outside := !st.IsTable && !closure[st.Name]
				switch {
				case outside && (g.Json != nil || g.Id != "0"):
					t.Errorf("%s: %s.%s is outside every table closure and carries a text key or a wire id (§8.2)", dir, st.Name, f.Name)
				case !outside && (g.Json == nil || *g.Json != ir.TableFieldJsonKey(f) || g.Id != fmt.Sprintf("%x", ir.TableFieldWireId(f))):
					t.Errorf("%s: %s.%s carries key %v id %s; the IR has %q %x", dir, st.Name, f.Name, g.Json, g.Id, ir.TableFieldJsonKey(f), ir.TableFieldWireId(f))
				}
				if f.Type.Kind == ir.TNamed {
					switch ref := f.Type.Ref.(type) {
					case *ir.Struct:
						if g.Table != ref.Name {
							t.Errorf("%s: %s.%s names descriptor %q; it nests %s", dir, st.Name, f.Name, g.Table, ref.Name)
						}
					case *ir.Enum, *ir.Union, *ir.Flags:
						if g.Vocabulary != f.Type.Name {
							t.Errorf("%s: %s.%s names vocabulary %q; it draws from %s", dir, st.Name, f.Name, g.Vocabulary, f.Type.Name)
						}
					}
				}
			}
		}
		for name, c := range u.Consts {
			g, ok := d.Consts[name]
			if !ok || g.TypeName != c.Storage || g.IsFloat != c.IsFloat || g.Doc != c.Doc || !viewTagsEqual(g.Tags, c.Tags) {
				t.Errorf("%s: constant %s reads %+v; the IR has %s float=%v doc %q tags %v", dir, name, g, c.Storage, c.IsFloat, c.Doc, c.Tags)
			}
		}
		reached := ir.TableClosureVocabulary(u)
		for name, e := range u.Enums {
			g := d.Vocab[name]
			if len(g.Variants) != len(e.Variants)+1 || g.Max != e.Max {
				t.Errorf("%s: enum %s carries %d rows max %d; the IR has %d variants and None, max %d", dir, name, len(g.Variants), g.Max, len(e.Variants), e.Max)
				continue
			}
			for i := range e.Variants {
				r := g.Variants[i+1]
				id := uint64(0)
				if reached[name] {
					id = ir.TableWireId(e.VariantWireName(i))
				}
				if r.Name != e.Variants[i] || r.Id != fmt.Sprintf("%x", id) || r.Doc != e.VariantDocs[i] || !viewTagsEqual(r.Tags, e.VariantTags[i]) {
					t.Errorf("%s: enum %s row %d reads %+v; the IR has %s id %x doc %q tags %v", dir, name, i+1, r, e.Variants[i], id, e.VariantDocs[i], e.VariantTags[i])
				}
			}
		}
		switch dir {
		case "examples":
			viewWrittenOut(t, d, "Grade", "enum", []string{"progression"}, []viewDumpVariant{
				{Value: 0, Name: "None"},
				{Value: 1, Name: "Bronze"},
				{Value: 2, Name: "Silver"},
				{Value: 3, Name: "Gold", Doc: "The top rank. A profile that reaches it keeps it.", Tags: []string{"celebrate", "rare", "announce"}},
			})
			viewWrittenOut(t, d, "Effect", "union", []string{"gameplay"}, []viewDumpVariant{
				{Value: 0, Name: "None"},
				{Value: 1, Name: "buff", Payload: "Buff", Doc: "A multiplier that rides while the effect is live.", Tags: []string{"positive"}},
				{Value: 2, Name: "debuff", Payload: "Debuff"},
			})
			w := d.Desc["WeaponConfig"]
			if w.Doc != "One weapon's tuning. A designer edits this table and nothing else." || !viewTagsEqual(w.Tags, []string{"designer_facing"}) {
				t.Errorf("examples: WeaponConfig carries doc %q tags %v", w.Doc, w.Tags)
			}
			if len(w.Fields) == 0 || w.Fields[0].Name != "damage" || !viewTagsEqual(w.Fields[0].Tags, []string{"ui_slider", "hot", "tuning"}) ||
				!strings.Contains(w.Fields[0].Doc, "\n") || !strings.Contains(w.Fields[0].Doc, `"hardness \ hit"`) {
				t.Errorf("examples: WeaponConfig.damage does not carry its multi-line doc with its backslash and its three tags: %+v", w.Fields[0])
			}
			if c := d.Consts["MaxLoadoutSlots"]; c.Int != "8" || !viewTagsEqual(c.Tags, []string{"tuning"}) || !strings.HasPrefix(c.Doc, "The slots a loadout admits.") {
				t.Errorf("examples: MaxLoadoutSlots reads %+v", c)
			}
		case "arms":
			viewWrittenOut(t, d, "Carry", "union", nil, []viewDumpVariant{
				{Value: 0, Name: "None"},
				{Value: 1, Name: "leaf", Payload: "Leaf"},
				{Value: 2, Name: "plain", Doc: "The arm that names no record: a scalar in the union's own overlay.", Tags: []string{"overlay"}},
			})
			carry := d.Vocab["Carry"]
			plain := carry.Variants[2].Field
			_, _, _, base := ir.UnionLayout(u, u.TableUnions["Carry"])
			if plain == nil || plain.Kind != tkI32 || plain.Offset != base || plain.ElementSize != 4 || carry.PayloadOffset != base ||
				plain.Doc != carry.Variants[2].Doc || !viewTagsEqual(plain.Tags, carry.Variants[2].Tags) {
				t.Errorf("arms: Carry.plain is a general arm: an int32 field descriptor at the payload base %d with the arm's doc and tags; it reads %+v (base %d)", base, plain, carry.PayloadOffset)
			}
			if carry.Variants[1].Field != nil || carry.Variants[0].Field != nil {
				t.Errorf("arms: Carry's None and leaf rows carry a field descriptor; exactly one of payload and field is set, and neither on None")
			}
		}
	}
}

// viewWrittenOut holds one vocabulary row by row to the names, payloads, docs
// and tags the test spells (docs/SPEC-TABLES.md §8.1, §8.3): None's id is 0
// and every other row's is a wire id (§5), which the IR check above pins.
func viewWrittenOut(t *testing.T, d viewDump, name, kind string, tags []string, want []viewDumpVariant) {
	t.Helper()
	g, ok := d.Vocab[name]
	if !ok || g.Kind != kind || !viewTagsEqual(g.Tags, tags) || len(g.Variants) != len(want) {
		t.Errorf("%s: the %s row reads %+v; want %d rows tagged %v", name, kind, g, len(want), tags)
		return
	}
	for i, w := range want {
		r := g.Variants[i]
		id := "0"
		if r.Value != w.Value || r.Name != w.Name || r.Payload != w.Payload || r.Doc != w.Doc || !viewTagsEqual(r.Tags, w.Tags) {
			t.Errorf("%s row %d reads %+v; want %+v", name, i, r, w)
		}
		if i == 0 && r.Id != id {
			t.Errorf("%s row 0 (None) carries id %s; no table-wire identity is 0 (§8.1)", name, r.Id)
		}
		if i > 0 && r.Id == "0" {
			t.Errorf("%s row %d (%s) carries no wire id, and a table closure reaches it", name, i, r.Name)
		}
	}
}
