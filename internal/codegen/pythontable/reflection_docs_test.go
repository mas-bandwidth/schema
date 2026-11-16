package pythontable

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const reflectionDocsSchema = `package core
union Choice
{
    /// Arm documentation.
    number int32 | arm_tag
}
/// Record documentation.
table Record | visible
{
    /// Value documentation.
    value int32 | tagged, other
    choice Choice
}
`

const reflectionDocsProbe = pyrun.ImportPrelude + `import json
from core import Record, Choice
d = Record.descriptor
f = d.fields[0]
arm = Choice.descriptor.arms[0].field
assert arm is not None
import core
view = core.UnitView()
choice = next(row for row in view.unions if row.name == "Choice")
assert choice.variants[1].field is arm
print(json.dumps([d.doc, list(d.tags), f.doc, list(f.tags), arm.doc, list(arm.tags)], ensure_ascii=True))
`

func reflectionDocsOutput(t *testing.T, files map[string][]byte, floor bool) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	if floor {
		return strings.TrimSpace(string(pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(reflectionDocsProbe)})))
	}
	return strings.TrimSpace(string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(reflectionDocsProbe)})))
}

func reflectionDocsWant(t *testing.T) string {
	t.Helper()
	u := loadUnit(t, reflectionDocsSchema)
	st := u.Tables["Record"]
	arm := u.TableUnions["Choice"].Variants[0]
	want, err := json.Marshal([]any{st.Doc, st.Tags, st.Fields[0].Doc, st.Fields[0].Tags, arm.Doc, arm.Tags})
	if err != nil {
		t.Fatal(err)
	}
	return string(want)
}

func reflectionDocsCompare(t *testing.T, files map[string][]byte, floor bool) bool {
	t.Helper()
	got := reflectionDocsOutput(t, files, floor)
	var value any
	if err := json.Unmarshal([]byte(got), &value); err != nil {
		t.Fatal(err)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(normalized) == reflectionDocsWant(t)
}

func TestPythonReflectionDescriptorDocs(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, floor := range []bool{false, true} {
		if !reflectionDocsCompare(t, files, floor) {
			t.Errorf("floor=%v: descriptor annotations differ from IR", floor)
		}
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "core"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}

}

func TestPythonReflectionDescriptorDocsNegativeControl(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, from, to string }{
		{"table-doc", `doc="Record documentation.", tags=("visible", )`, `doc="drift", tags=("visible", )`},
		{"field-doc", `doc="Value documentation.", tags=("tagged", "other", )`, `doc="drift", tags=("tagged", "other", )`},
		{"field-tags", `doc="Value documentation.", tags=("tagged", "other", )`, `doc="Value documentation.", tags=("other", "tagged", )`},
		{"arm-doc", `field=FieldDesc(name="number", doc="Arm documentation.", tags=("arm_tag", )`, `field=FieldDesc(name="number", doc="drift", tags=("arm_tag", )`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, reflectionDocsSchema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range files {
				if !strings.HasSuffix(name, "/Core.py") {
					continue
				}
				if !strings.Contains(string(data), tc.from) {
					t.Fatal("control could not find annotation")
				}
				files[name] = []byte(strings.Replace(string(data), tc.from, tc.to, 1))
				if reflectionDocsCompare(t, files, false) {
					t.Fatalf("%s drift passed the descriptor comparison", tc.name)
				}
				return
			}
			t.Fatal("control could not find generated module")
		})
	}
}

const reflectionAnnotationAgreementProbe = pyrun.ImportPrelude + `import core
v=core.UnitView()
table=next(row for row in v.tables if row.name == 'Record')
assert table.doc == table.type.doc and table.tags == table.type.tags, 'table annotation drift'
union=next(row for row in v.unions if row.name == 'Choice')
arm=union.variants[1]
assert arm.field is not None
assert arm.doc == arm.field.doc and arm.tags == arm.field.tags, 'arm annotation drift'
`

func TestPythonReflectionAnnotationAgreement(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(reflectionAnnotationAgreementProbe)}
	pyrun.Run(t, dir, "main.py", inputs)
	pyrun.RunFloor(t, dir, "main.py", inputs)
}

func TestPythonReflectionAnnotationAgreementNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	path := "core/_schema/unit_view.py"
	src := string(files[path])
	from := `"Arm documentation.", ("arm_tag", ))`
	if !strings.Contains(src, from) {
		t.Fatal("arm annotation control anchor absent")
	}
	files[path] = []byte(strings.Replace(src, from, `"drift", ("arm_tag", ))`, 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(reflectionAnnotationAgreementProbe)})
	if err == nil || !strings.Contains(stderr, "arm annotation drift") {
		t.Fatalf("arm metadata drift escaped the agreement gate: %v\n%s", err, stderr)
	}
}

func TestPythonReflectionAnnotationEscapes(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, `package accents
/// Quotes " and a backslash \ with café.
/// A second line.
table Record | first, second
{
    /// Path C:\tools\test and "quoted".
    value int32 | alpha, beta
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Record"]
	want, err := json.Marshal([]any{st.Doc, st.Tags, st.Fields[0].Doc, st.Fields[0].Tags})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import json
import accents
d=accents.Record.descriptor
f=d.fields[0]
print(json.dumps([d.doc, list(d.tags), f.doc, list(f.tags)], ensure_ascii=True))
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, floor := range []bool{false, true} {
		var got []byte
		if floor {
			got = pyrun.RunFloor(t, dir, "main.py", inputs)
		} else {
			got = pyrun.Run(t, dir, "main.py", inputs)
		}
		var value any
		if err := json.Unmarshal(got, &value); err != nil {
			t.Fatal(err)
		}
		normalized, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(normalized) != string(want) {
			t.Errorf("floor=%v annotations differ: got %s want %s", floor, normalized, want)
		}
	}
}

const reflectionFieldVocabularySchema = `package fieldvocab
enum Grade { Red, Blue }
flags Cap { Fast, Slow }
table Child { value int32 }
union Choice {
    leaf Child
    amount int32
}
table Root {
    grade Grade
    caps Cap
    keyed [Grade]int32
    grades [2]Grade
    choice Choice
}
`

func reflectionFieldVocabularyProbe(redID, armID uint64) string {
	return pyrun.ImportPrelude + fmt.Sprintf(`import fieldvocab
r = fieldvocab.Root.descriptor
g = r.field('grade')
f = r.field('caps')
k = r.field('keyed')
u = r.field('choice')
a = r.field('grades')
assert g is not None and f is not None and k is not None and u is not None and a is not None
assert g.variant_name is not None and g.variant_id is not None
assert g.key_name is None and g.key_id is None
assert f.variant_name is not None and f.variant_id is None
assert k.key_name is not None and k.key_id is not None
assert u.variant_name is not None and u.variant_id is not None
assert g.variant_name(0) == 'None' and g.variant_name(1) == 'Red', 'grade-name drift'
assert g.variant_id(0) == 0 and g.variant_id(1) == %d
assert f.variant_name(0) == 'Fast' and f.variant_name(1) == 'Slow'
assert k.key_name(0) == 'None' and k.key_name(1) == 'Red' and k.key_id(1) == %d
assert a.element is not None and a.element.variant_name is not None
assert a.element.variant_name(1) == 'Red'
assert u.variant_name(0) == 'None' and u.variant_name(2) == 'amount' and u.variant_id(2) == %d
assert u.union_desc is fieldvocab.Choice.descriptor, 'union lookup drift'
assert u.union_desc.arms[0].target_desc is fieldvocab.Child.descriptor
assert u.union_desc.arms[1].field is not None
`, redID, redID, armID)
}

func TestPythonReflectionFieldVocabulary(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionFieldVocabularySchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := reflectionFieldVocabularyProbe(ir.TableWireId(u.Enums["Grade"].VariantWireName(0)), ir.TableWireId(u.TableUnions["Choice"].Variants[1].WireName()))
	inputs := map[string][]byte{"main.py": []byte(probe)}
	pyrun.Run(t, dir, "main.py", inputs)
	pyrun.RunFloor(t, dir, "main.py", inputs)
}

func TestPythonReflectionFieldVocabularyNegativeControl(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to, reason string }{
		{"enum-name", `variant_names=("Red", "Blue", )`, `variant_names=("Drift", "Blue", )`, "grade-name drift"},
		{"union-target", `target="Choice"`, `target="Missing"`, "union lookup drift"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, reflectionFieldVocabularySchema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			path := "fieldvocab/Core.py"
			src := string(files[path])
			if !strings.Contains(src, tc.from) {
				t.Fatal("vocabulary control anchor absent")
			}
			files[path] = []byte(strings.Replace(src, tc.from, tc.to, 1))
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := reflectionFieldVocabularyProbe(ir.TableWireId(u.Enums["Grade"].VariantWireName(0)), ir.TableWireId(u.TableUnions["Choice"].Variants[1].WireName()))
			_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, tc.reason) {
				t.Fatalf("%s sabotage escaped: %v\n%s", tc.name, err, stderr)
			}
		})
	}
}
