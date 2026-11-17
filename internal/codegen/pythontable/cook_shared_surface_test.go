package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonCookSharedBlobSurface(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters and conformance driver")
	u := loadUnit(t, `package sharedsurface
 table Root {
  left *bytes
  right *bytes
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	generated := filepath.Join(dir, "generated")
	writeTree(t, filepath.Join(generated, "unit"), files)
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	for i := range root.Fields {
		root.Fields[i].Cell.Blob = &tabletext.Blob{Data: []byte("data")}
	}
	wire, err := tablewire.EncodeVariable(m, root)
	if err != nil {
		t.Fatal(err)
	}
	wirePath := filepath.Join(dir, "input.wire")
	if err := os.WriteFile(wirePath, wire, 0600); err != nil {
		t.Fatal(err)
	}
	shared := &tabletext.Blob{Data: []byte("data")}
	for i := range root.Fields {
		root.Fields[i].Cell.Blob = shared
	}
	var want [2][]byte
	for i, big := range []bool{false, true} {
		want[i], err = tablecook.Cook(m, root, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(dir, "manifest")
	text := fmt.Sprintf("instance value unit Root %s\ncook-write value - left right\n", wirePath)
	if err := os.WriteFile(manifest, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	driver, err := filepath.Abs("../../../test/conformance/python/main.py")
	if err != nil {
		t.Fatal(err)
	}
	runDriver := func(t *testing.T, runner func(testing.TB, string, string, map[string][]byte) []byte, path, out string) {
		t.Helper()
		if err := os.Mkdir(out, 0700); err != nil {
			t.Fatal(err)
		}
		script := fmt.Sprintf("import os, runpy, sys\nos.environ['PYTHON_CONFORMANCE_GENERATED'] = %s\nsys.argv = [%s, %s, 'cook-write', %s]\nrunpy.run_path(%s, run_name='__main__')\n", strconv.Quote(generated), strconv.Quote(path), strconv.Quote(manifest), strconv.Quote(out), strconv.Quote(path))
		runner(t, dir, "driver_probe.py", map[string][]byte{"driver_probe.py": []byte(script)})
	}
	for pin, runner := range cookPins() {
		out := filepath.Join(dir, pin)
		runDriver(t, runner, driver, out)
		for i, suffix := range []string{"", "-be"} {
			got, err := os.ReadFile(filepath.Join(out, "value"+suffix))
			if err != nil || string(got) != string(want[i]) {
				t.Errorf("%s shared surface %s: %v", pin, suffix, err)
			}
		}
	}
	t.Run("alias-negative-control", func(t *testing.T) {
		t.Parallel()
		copyDir := t.TempDir()
		surfaces := filepath.Join(copyDir, "surfaces")
		if err := os.Mkdir(surfaces, 0700); err != nil {
			t.Fatal(err)
		}
		main, err := os.ReadFile(driver)
		if err != nil {
			t.Fatal(err)
		}
		copyDriver := filepath.Join(copyDir, "main.py")
		if err := os.WriteFile(copyDriver, main, 0600); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"_common.py", "_blocks.py", "_cooks.py", "cook.py", "cook-foreign.py", "cook-forgery.py", "cook-write.py"} {
			raw, err := os.ReadFile(filepath.Join(filepath.Dir(driver), "surfaces", file))
			if err != nil {
				t.Fatal(err)
			}
			if file == "cook-write.py" {
				const anchor = "shared = package.BlobRef(body)"
				if !strings.Contains(string(raw), anchor) {
					t.Fatal("alias control anchor absent")
				}
				raw = []byte(strings.Replace(string(raw), anchor, "shared = body", 1))
			}
			if err := os.WriteFile(filepath.Join(surfaces, file), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for pin, runner := range cookPins() {
			out := filepath.Join(copyDir, pin)
			runDriver(t, runner, copyDriver, out)
			got, err := os.ReadFile(filepath.Join(out, "value"))
			if err != nil || string(got) == string(want[0]) {
				t.Errorf("%s alias control missed fault: %v", pin, err)
			}
		}
	})
}
