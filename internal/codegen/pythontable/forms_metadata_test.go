package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const formsMetadataSchema = `package formsprobe
fixed table A | forms="wire" { n int32 }
fixed table B | forms="wire,block" { child A }
fixed table C { n int32 }
`
const formsMetadataProbe = `import formsprobe as p
from formsprobe._schema.blockbuild import Begin
from formsprobe._schema.block import BlockOpen
from formsprobe import InvalidValue, Refused
assert p.A.descriptor.forms == ("wire",), "A forms"
assert p.B.descriptor.forms == ("wire", "block"), "B forms"
assert p.C.descriptor.forms == ("wire", "json", "block", "cook"), "default forms"
assert p.A.block is None, "excluded block emitted"
assert p.B.block is not None and p.C.block is not None, "allowed block missing"
assert getattr(p.A, "block_row", None) is not None, "nested row removed"
try: Begin(p, "A")
except InvalidValue: pass
else: raise AssertionError("excluded block builder accepted")
try: BlockOpen(p, "A", b"")
except Refused as e: assert e.reason == "not_a_cook"
else: raise AssertionError("excluded block open accepted")
block = Begin(p, "B")
assert any(field.name == "child" for field in block.projection.fields), "nested child missing"
assert BlockOpen(p, "B", block.bytes).name == "B"
print("ok")
`

func TestPythonFormsBlockMetadata(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python declared forms metadata")
	u := loadUnit(t, formsMetadataSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "formsprobe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + formsMetadataProbe)}
	for _, out := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
		if string(out) != "ok\n" {
			t.Fatalf("forms: %q", out)
		}
	}
}

func TestPythonFormsBlockMetadataNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python declared forms negative control")
	u := loadUnit(t, formsMetadataSchema)
	for _, tc := range []struct{ anchor, replacement, failure string }{
		{`forms=("wire",),`, `forms=("wire", "block"),`, "A forms"},
		{`A.block = None`, `A.block = object()`, "excluded block emitted"},
		{`A.block_row = BlockRecord(`, `A.discarded_block_row = BlockRecord(`, "nested row removed"},
	} {
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		path := "formsprobe/Records.py"
		if strings.Count(string(files[path]), tc.anchor) != 1 {
			t.Fatalf("control anchor missing or repeated: %s", tc.anchor)
		}
		files[path] = []byte(strings.Replace(string(files[path]), tc.anchor, tc.replacement, 1))
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := t.TempDir()
			writeTree(t, dir, files)
			_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + formsMetadataProbe)})
			if err == nil || !strings.Contains(stderr, tc.failure) {
				t.Fatalf("control %s: %v %s", tc.anchor, err, stderr)
			}
		}
	}
}
