package pythontable

import (
	"bytes"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// These probes exercise the variable wire and its keyed-field evolution controls.
// Declared fixed roots use form 3 and are covered by the fixed-form proof suite.
const pyKeyedEnumSchema = `package enumkey

enum Slot { head, body, feet }
enum Grade {
    Low
    Mid
    Old | deprecated
}
table Root {
    ranks [Slot]Grade
    tail int32
}
`

func TestPythonVtKeyedEnumDescriptor(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, pyKeyedEnumSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	source := string(files["enumkey/Core.py"])
	if !strings.Contains(source, `codec="keyed"`) {
		t.Fatal("enum-valued keyed field is not carried by the keyed codec")
	}
	if !strings.Contains(source, `codec="enum"`) {
		t.Fatal("the keyed element is not carried by the enum codec")
	}
}

const pyKeyedEnumHostedSchema = `package enumkey

enum Slot { head, body, feet }
enum Grade {
    Low
    Mid
    Old | deprecated
}
type Scores { ranks [Slot]Grade }
table Holder {
    values Scores
    tail int32
}
`

const pyKeyedEnumProbe = `import json
import enumkey
wire = open('wire', 'rb').read()
mode = open('mode').read()
values = json.loads(open('values').read())
cls = enumkey.Root if mode == 'wrapper' else enumkey.Holder
r = cls()
if mode == 'wrapper':
    for i, value in enumerate(values, 1): r.ranks[i] = value
    r.tail = 7
else:
    r.values.ranks = values
    r.tail = 7
assert r.save() == wire, 'python writer differs from Go engine'
assert r.measure() == len(wire)
loaded = cls.load(wire)
assert loaded == r, 'python read differs from source'
assert loaded.save() == wire, 'python resave differs from Go engine'
print('ok')
`

func TestPythonVtKeyedEnumRoundTripFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "C++ oracle and both pinned Python interpreters")
	cases := []struct{ name, schema, root, text, mode, values string }{
		{"default", pyKeyedEnumSchema, "Root", `{"tail":7}`, "wrapper", `[0,0,0]`},
		{"sparse", pyKeyedEnumSchema, "Root", `{"ranks":{"head":"Low","feet":"Mid"},"tail":7}`, "wrapper", `[1,0,2]`},
		{"full", pyKeyedEnumSchema, "Root", `{"ranks":{"head":"Mid","body":"Low","feet":"Mid"},"tail":7}`, "wrapper", `[2,1,2]`},
		{"hosted-list", pyKeyedEnumHostedSchema, "Holder", `{"values":{"ranks":{"head":"Low","feet":"Mid"}},"tail":7}`, "hosted", `[1,0,2]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, tc.schema)
			want := wideEngineWire(t, u, tc.root, tc.text)
			cpp := cppWideWire(t, u, wideVTConstruct{name: tc.name, schema: tc.schema, root: tc.root, json: tc.text})
			if !bytes.Equal(cpp, want) {
				t.Fatalf("C++ and Go bytes differ: cpp=%x go=%x", cpp, want)
			}
			got := pyKeyedRun(t, u, pyKeyedEnumProbe, map[string][]byte{"wire": want, "mode": []byte(tc.mode), "values": []byte(tc.values)}, nil)
			if strings.TrimSpace(got) != "ok" {
				t.Fatalf("probe: %s", got)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("strict mypy: %v\n%s", err, out)
			}
		})
	}
}

func pyKeyedEnumWire(u *ir.Unit, keyName, variantName string, rawPayload []byte) []byte {
	st := u.Tables["Root"]
	fieldID := ir.TableFieldWireId(st.Fields[0])
	keyID := ir.TableWireId(keyName)
	b := &pyKeyedBuild{}
	b.ref(fieldID)
	b.ref(keyID)
	if variantName != "" {
		rawPayload = pyLeb(b.ref(ir.TableWireId(variantName)))
	}
	b.keyedField(fieldID, 16, 30, 1, []pyKeyedSlot{{key: keyID, payload: rawPayload}}, nil)
	return b.file()
}

func pyKeyedEnumGoAnswer(t *testing.T, u *ir.Unit, wire []byte) (string, []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["Root"])
	var report tabletext.Report
	ok, _ := tablewire.DecodeVariable(m, value, wire, &report)
	if !ok {
		return "refused", nil
	}
	field, found := value.FieldByKey("ranks")
	if !found || len(field.Elems) != 3 {
		t.Fatal("Go engine did not establish the keyed slots")
	}
	canonical, err := tablewire.EncodeVariable(m, value)
	if err != nil {
		t.Fatalf("Go re-encode: %v", err)
	}
	return fmt.Sprintf("read %d %d", report.Unknown, field.Elems[0].U), canonical
}

const pyKeyedEnumReadProbe = `from enumkey import LoadReport, Refused, Root
wire = open('wire','rb').read()
report = LoadReport()
report.unknown = 42
try:
    r = Root.load(wire, report=report)
except Refused:
    assert report.unknown == 42, 'refused read mutated the report'
    print('refused')
else:
    print('read', report.unknown, r.ranks[1], r.save().hex())
`

func TestPythonVtKeyedEnumEvolutionFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, pyKeyedEnumSchema)
	cases := []struct {
		name, key, value string
		raw              []byte
		want             string
	}{
		{"known", "head", "Low", nil, "read 0 1"},
		{"unknown-value", "head", "Future", nil, "read 1 0"},
		{"deprecated-value", "head", "Old", nil, "read 0 0"},
		{"unknown-key", "missing", "Low", nil, "read 1 0"},
		{"reference-past-table", "head", "", []byte{99}, "refused"},
		{"nonminimal-reference", "head", "", []byte{0x80, 0}, "refused"},
		{"trailing-byte", "head", "", []byte{0, 0}, "refused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wire := pyKeyedEnumWire(u, tc.key, tc.value, tc.raw)
			goAnswer, canonical := pyKeyedEnumGoAnswer(t, u, wire)
			if goAnswer != tc.want {
				t.Fatalf("Go engine: %s, expected %s (wire %x)", goAnswer, tc.want, wire)
			}
			want := goAnswer
			if canonical != nil {
				want += fmt.Sprintf(" %x", canonical)
			}
			got := strings.TrimSpace(pyKeyedRun(t, u, pyKeyedEnumReadProbe, map[string][]byte{"wire": wire}, nil))
			if got != want {
				t.Fatalf("Python: %s, Go: %s", got, want)
			}
		})
	}
}

const pyKeyedEnumWriteRefusalProbe = `import enumkey
mode = open('mode').read()
for label, bad in [('deprecated', enumkey.Grade.Old), ('unknown', 4), ('bool', True), ('negative', -1)]:
    r=enumkey.Root() if mode == 'wrapper' else enumkey.Holder()
    slots = r.ranks if mode == 'wrapper' else r.values.ranks
    if mode == 'wrapper':
        slots[1]=enumkey.Grade.Low
        slots[3]=bad
        expected_path = 'ranks[3]'
    else:
        slots[0]=enumkey.Grade.Low
        slots[2]=bad
        expected_path = 'values.ranks[3]'
    before=repr(r)
    try:
        r.save()
    except enumkey.InvalidValue as exc:
        assert exc.path == expected_path and repr(r) == before, label
        print(label, 'refused')
    else:
        raise AssertionError(label + ' write accepted')
`

func TestPythonVtKeyedEnumWriteRefusalsFunctional(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ mode, schema, root string }{{"wrapper", pyKeyedEnumSchema, "Root"}, {"hosted", pyKeyedEnumHostedSchema, "Holder"}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, tc.schema)
			for _, bad := range []uint64{3, 4} {
				m := tabletext.NewModel(u)
				value := m.New(u.Tables[tc.root])
				if tc.mode == "hosted" {
					field, ok := value.FieldByKey("values")
					if !ok || field.Cell.Tab == nil {
						t.Fatal("missing hosted value")
					}
					value = field.Cell.Tab
				}
				field, ok := value.FieldByKey("ranks")
				if !ok {
					t.Fatal("missing ranks")
				}
				field.Elems[2].U = bad
				if _, err := tablewire.EncodeVariable(m, m.New(u.Tables[tc.root])); err != nil {
					t.Fatalf("default Go value refused: %v", err)
				}
				// Check the actual invalid root, not a newly constructed replacement.
				root := m.New(u.Tables[tc.root])
				if tc.mode == "wrapper" {
					root = value
				} else {
					parent, _ := root.FieldByKey("values")
					parent.Cell.Tab = value
				}
				if _, err := tablewire.EncodeVariable(m, root); err == nil {
					t.Errorf("Go writer accepted enum ordinal %d", bad)
				}
			}
			got := pyKeyedRun(t, u, pyKeyedEnumWriteRefusalProbe, map[string][]byte{"mode": []byte(tc.mode)}, nil)
			for _, label := range []string{"deprecated", "unknown", "bool", "negative"} {
				if !strings.Contains(got, label+" refused\n") {
					t.Errorf("missing %s: %s", label, got)
				}
			}
		})
	}
}

const pyKeyedEnumDescriptorReadProbe = `from enumkey import Root
r=Root.load(open('wire','rb').read())
assert r.ranks[1] == 1, 'enum descriptor drift'
`

const pyKeyedEnumDeprecatedReadProbe = `from enumkey import Root
r=Root.load(open('wire','rb').read())
assert r.ranks[1] == 0, 'deprecated enum drift'
`

const pyKeyedEnumUnknownKeyProbe = `from enumkey import Root, Refused
try:
    Root.load(open('wire','rb').read())
except Refused:
    pass
else:
    raise AssertionError('damaged unknown key accepted')
`

func TestPythonVtKeyedEnumNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, pyKeyedEnumSchema)
	text := `{"ranks":{"head":"Low","feet":"Mid"},"tail":7}`
	good := wideEngineWire(t, u, "Root", text)
	old := pyKeyedEnumWire(u, "head", "Old", nil)
	damaged := pyKeyedEnumWire(u, "missing", "", []byte{99})
	if verdict, _ := pyKeyedEnumGoAnswer(t, u, damaged); verdict != "refused" {
		t.Fatal("Go engine accepted the damaged unknown key control")
	}
	for _, tc := range []struct {
		name, file, anchor, replacement, probe, reason string
		inputs                                         map[string][]byte
	}{
		{"writer-id", "_schema/kind_keyed.py", `codec.write_element(Writer(element), elem, current, ctx)`, `Writer(element).byte(current)`, pyKeyedEnumProbe, "python writer differs from Go engine", map[string][]byte{"wire": good, "mode": []byte("wrapper"), "values": []byte(`[1,0,2]`)}},
		{"element-ids", "Core.py", `variant_ids=` + variantIds(u.Tables["Root"].Fields[0]), `variant_ids=()`, pyKeyedEnumDescriptorReadProbe, "enum descriptor drift", map[string][]byte{"wire": good}},
		{"deprecated-read", "_schema/kind_enum.py", `if ordinal in fd.deprecated:`, `if False and ordinal in fd.deprecated:`, pyKeyedEnumDeprecatedReadProbe, "deprecated enum drift", map[string][]byte{"wire": old}},
		{"deprecated-write", "_schema/kind_enum.py", `if value in fd.deprecated:`, `if False and value in fd.deprecated:`, pyKeyedEnumWriteRefusalProbe, "deprecated write accepted", map[string][]byte{"mode": []byte("wrapper")}},
		{"unknown-key-check", "_schema/kind_keyed.py", `_skip_content(element, elem_kind, ctx, ctx.depth)`, `pass`, pyKeyedEnumUnknownKeyProbe, "damaged unknown key accepted", map[string][]byte{"wire": damaged}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			path := "enumkey/" + tc.file
			src := string(files[path])
			start := 0
			if tc.name == "element-ids" {
				start = strings.Index(src, "elem=FieldDesc(")
				if start < 0 {
					t.Fatal("element descriptor anchor absent")
				}
			}
			index := strings.Index(src[start:], tc.anchor)
			if index < 0 {
				t.Fatalf("control anchor absent: %s", tc.anchor)
			}
			index += start
			files[path] = []byte(src[:index] + tc.replacement + src[index+len(tc.anchor):])
			dir := t.TempDir()
			writeTree(t, dir, files)
			inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + tc.probe)}
			maps.Copy(inputs, tc.inputs)
			for _, pin := range []struct {
				name string
				path func(testing.TB) string
			}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
				_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", inputs)
				if err == nil || !strings.Contains(stderr, tc.reason) {
					t.Errorf("%s: %s control did not fail by name: %v\n%s", pin.name, tc.name, err, stderr)
				}
			}
		})
	}
}
