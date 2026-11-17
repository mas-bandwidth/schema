package pythontable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestPythonForm1ArrayPriorWriters uses checked old required and optional
// declarations: their present-empty field remains legal to a plain reader.
func TestPythonForm1ArrayPriorWriters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	reader := loadUnit(t, `package compat
table Root { items []int32 }`)
	oldRequired := loadUnit(t, `package compat
table Root { items []int32 | required }`)
	oldOptional := loadUnit(t, `package compat
table Root { items ?[..4]int32 }`)
	encode := func(uModel *tabletext.Model, value string) []byte {
		inst := uModel.New(uModel.Lookup("Root"))
		var rep tabletext.Report
		if !uModel.Read(inst, []byte(value), &rep) || rep.Refused {
			t.Fatalf("old writer JSON refused: %+v", rep)
		}
		out, err := tablewire.EncodeVariable(uModel, inst)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	requiredWire := encode(tabletext.NewModel(oldRequired), `{"items":[]}`)
	optionalWire := encode(tabletext.NewModel(oldOptional), `{"items":[]}`)
	canonical := encode(tabletext.NewModel(reader), `{}`)
	if string(requiredWire) == string(canonical) || string(optionalWire) == string(canonical) {
		t.Fatal("old writer did not actually store present-empty")
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from compat import Root
with open("wire", "rb") as handle: data=handle.read()
with open("canonical", "rb") as handle: canonical=handle.read()
v=Root.load(data)
assert v.items == [], "prior_empty_value"
assert v.save() == canonical, "canonical_resave"
print("prior_empty_ok")
`
	for _, planted := range []bool{false, true} {
		copyFiles := maps.Clone(files)
		if planted {
			const path = "compat/_schema/kind_array.py"
			const anchor = "        return items\n"
			const insert = "        if count == 0 and not fd.optional:\n            raise Refused(\"non_canonical\", \"planted prior-empty refusal\")\n        return items\n"
			if strings.Count(string(copyFiles[path]), anchor) != 1 {
				t.Fatal("prior-empty negative control anchor moved")
			}
			copyFiles[path] = []byte(strings.Replace(string(copyFiles[path]), anchor, insert, 1))
		}
		dir := t.TempDir()
		writeTree(t, dir, copyFiles)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			for _, old := range []struct {
				name string
				wire []byte
			}{{"required", requiredWire}, {"optional", optionalWire}} {
				out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": old.wire, "canonical": canonical})
				if !planted && (err != nil || strings.TrimSpace(out) != "prior_empty_ok") {
					t.Errorf("%s/%s: %v %s %s", pin.name, old.name, err, out, stderr)
				}
				if planted && (err == nil || !strings.Contains(stderr, "planted prior-empty refusal")) {
					t.Errorf("%s/%s: planted refusal did not fire: %v %s", pin.name, old.name, err, stderr)
				}
			}
		}
	}
	requiredFiles, err := Generate(oldRequired)
	if err != nil {
		t.Fatal(err)
	}
	requiredDir := t.TempDir()
	writeTree(t, requiredDir, requiredFiles)
	for _, path := range []string{"compat", "compat/_schema"} {
		if out, err := pyrun.Analyze(t, requiredDir, path); err != nil {
			t.Fatalf("strict mypy %s: %v\n%s", path, err, out)
		}
	}
	requiredProbe := pyrun.ImportPrelude + `from compat import Root
with open("wire", "rb") as handle: data = handle.read()
v = Root.load(data)
assert v.items == [], "required_empty_value"
print("required_empty_ok")
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), requiredDir, "main.py", map[string][]byte{"main.py": []byte(requiredProbe), "wire": requiredWire})
		if err != nil || strings.TrimSpace(out) != "required_empty_ok" {
			t.Errorf("same required/%s: %v %s %s", pin.name, err, out, stderr)
		}
	}

}
