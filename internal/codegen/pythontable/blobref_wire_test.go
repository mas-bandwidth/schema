package pythontable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonBlobRefWire(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package blobrefwire
 union Link { data *bytes }
 table Child {
  data *bytes
  label *string
 }
 table Root {
  one Child
  two Child
  p *Child
  q *Child
  text *string
  choice Link
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	writeTree(t, dir, map[string][]byte{"typed.py": []byte(`from blobrefwire import BlobRef, Child
x = Child()
x.data = BlobRef(b"data")
`)})
	for _, module := range []string{"blobrefwire", "blobrefwire/_schema", "typed.py"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("strict mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from blobrefwire import BlobRef, Child, InvalidValue, Root
x = BlobRef(b"data")
r = Root(); r.one.data = x; r.two.data = x
try:
    r.save()
except InvalidValue as e:
    assert "blob_shared" in e.rule
else:
    raise AssertionError("repeated by-value blob was accepted")
r = Root(); r.one.data = BlobRef(b"data"); r.two.data = BlobRef(b"data"); r.save()
r = Root(); r.one.data = x; r.choice.type = 1; r.choice.data = x
try:
    r.save()
except InvalidValue as e:
    assert "blob_shared" in e.rule
else:
    raise AssertionError("shared union blob was accepted")
s = BlobRef("same")
r = Root(); r.one.label = s; r.two.label = s
try:
    r.save()
except InvalidValue as e:
    assert "blob_shared" in e.rule
else:
    raise AssertionError("repeated string blob was accepted")
r = Root(); node = Child(); node.data = x; r.p = node; r.q = node; r.save()
r = Root(); r.text = x
try:
    r.save()
except InvalidValue as e:
    assert "text" in e.path and "not a str" in e.rule
else:
    raise AssertionError("wrong blob payload kind was accepted")
try:
    x.value = b"new"
except AttributeError:
    pass
else:
    raise AssertionError("BlobRef value was mutable")
try:
    del x._value
except AttributeError:
    pass
else:
    raise AssertionError("BlobRef payload was deleted")
print("ok")
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Errorf("%s: %v %s %s", pin.name, err, out, stderr)
		}
	}
	t.Run("shared-refusal-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "blobrefwire/_schema/save.py"
		const anchor = "if identity in ctx.blob_refs:"
		source := string(planted[path])
		if !strings.Contains(source, anchor) {
			t.Fatal("blob refusal control anchor absent")
		}
		planted[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "repeated by-value blob was accepted") {
				t.Errorf("%s: refusal control did not detect the fault: %v %s", pin.name, err, stderr)
			}
		}
	})

	t.Run("shared-union-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "blobrefwire/_schema/kind_union.py"
		const anchor = "if identity in ctx.blob_refs:\n                    raise InvalidValue(_save._render(path), \"blob_shared\")"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("union blob refusal control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "if False:\n                    raise InvalidValue(_save._render(path), \"blob_shared\")", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "shared union blob was accepted") {
				t.Errorf("%s union refusal control: %v %s", pin.name, err, stderr)
			}
		}
	})

}
