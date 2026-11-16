package pythontable

import (
	"encoding/hex"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestPythonUnionBlobArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "arms"))
	testPythonUnionBlobArm(t, u, "2", `"hello"`, "text")
}

func TestPythonUnionBytesArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, `package armdemo
 union Reach { text *bytes }
 table Gate { reach Reach }
`)
	testPythonUnionBlobArm(t, u, "1", `b"hello"`, "text")
}

func TestPythonUnionNullBlobArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "arms"))
	testPythonUnionBlobArm(t, u, "2", "None", "text")
}

func TestPythonUnionEmptyBlobArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "arms"))
	testPythonUnionBlobArm(t, u, "2", `""`, "text")
}

func TestPythonUnionNullPointerArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "arms"))
	testPythonUnionBlobArm(t, u, "1", "None", "only")
}

func TestPythonUnionNullAndEmptyBytesArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, `package armdemo
 union Reach { text *bytes }
 table Gate { reach Reach }
`)
	testPythonUnionBlobArm(t, u, "1", "None", "text")
	testPythonUnionBlobArm(t, u, "1", `b""`, "text")
}

func testPythonUnionBlobArm(t *testing.T, u *ir.Unit, tag, value, arm string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"armdemo", "armdemo/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from armdemo import Gate
r = Gate(); r.reach.type = ` + tag + `; r.reach.` + arm + ` = ` + value + `
w = r.save(); value = Gate.load(w)
assert value.reach.` + arm + ` == ` + value + `
print(w.hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil {
			t.Fatalf("%s: %v %s", pin.name, err, stderr)
		}
		wire, err := hex.DecodeString(strings.TrimSpace(out))
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		root := m.New(m.Lookup("Gate"))
		var report tabletext.Report
		ok, err := tablewire.DecodeVariable(m, root, wire, &report)
		if err != nil || !ok || report.Refused {
			t.Fatalf("engine decode: %v %v %s", ok, err, report.Reason)
		}
		expected, err := tablewire.EncodeVariable(m, root)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(expected) != strings.TrimSpace(out) {
			t.Errorf("%s: wire differs from engine", pin.name)
		}
	}
	if value == "None" {
		t.Run("null-arm-negative-control", func(t *testing.T) {
			t.Parallel()
			planted := maps.Clone(files)
			const path = "armdemo/_schema/kind_union.py"
			const anchor = `if f.kind == 17 and value is None:`
			body := string(planted[path])
			if !strings.Contains(body, anchor) {
				t.Fatal("null control anchor absent")
			}
			planted[path] = []byte(strings.Replace(body, anchor, "if False:", 1))
			dir := t.TempDir()
			writeTree(t, dir, planted)
			for _, pin := range []struct {
				name string
				path func(testing.TB) string
			}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
				_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
				if err == nil || !strings.Contains(stderr, "node index") {
					t.Errorf("%s null control: %v %s", pin.name, err, stderr)
				}
			}
		})
		return
	}
	t.Run("negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "armdemo/_schema/kind_union.py"
		const anchor = `if f.codec in ("string_blob", "bytes_blob"):`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "if False:", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "byte buffer slot has no node index") {
				t.Errorf("%s control: %v %s", pin.name, err, stderr)
			}
		}
	})
}
