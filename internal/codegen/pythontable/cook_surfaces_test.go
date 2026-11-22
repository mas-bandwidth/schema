package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookForeignAndForgerySurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters and conformance driver")
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	oracle := makeCookOracle(t, u, files)
	dir := t.TempDir()
	generated := filepath.Join(dir, "generated")
	writeTree(t, filepath.Join(generated, "unit"), files)
	image := filepath.Join(dir, "valid.cook")
	if err := os.WriteFile(image, oracle.cooks["full"][0], 0600); err != nil {
		t.Fatal(err)
	}
	damaged := filepath.Join(dir, "damaged.cook")
	raw := append([]byte(nil), oracle.cooks["full"][0]...)
	raw[0] ^= 1
	if err := os.WriteFile(damaged, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output := string(pyrun.Run(t, filepath.Join(generated, "unit"), "wire_probe.py", map[string][]byte{"wire_probe.py": []byte(pyrun.ImportPrelude + cookWriteProbe)}))
	written, err := parseCookWrite(output)
	if err != nil {
		t.Fatal(err)
	}
	wire := filepath.Join(dir, "value.wire")
	if err := os.WriteFile(wire, written["full"].wire, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "manifest")
	body := fmt.Sprintf("cook example unit Root %s\nforgery valid cook example %s -1 0\nforgery damaged cook example %s -1 0\ninstance value unit Root %s\ncook-write value - - -\n", image, image, damaged, wire)
	if err := os.WriteFile(manifest, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	driver, err := filepath.Abs("../../../test/conformance/python/main.py")
	if err != nil {
		t.Fatal(err)
	}
	allCookPins := cookPins()
	if len(allCookPins) == 0 {
		t.Fatal("cookPins() is empty: the loop below would check nothing")
	}
	for pin, run := range allCookPins {
		for _, surface := range []string{"cook", "cook-foreign", "cook-forgery", "cook-write"} {
			outdir := filepath.Join(dir, pin+surface)
			if err := os.Mkdir(outdir, 0700); err != nil {
				t.Fatal(err)
			}
			script := fmt.Sprintf("import os, runpy, sys\nos.environ['PYTHON_CONFORMANCE_GENERATED'] = %s\nsys.argv = [%s, %s, %s, %s]\nrunpy.run_path(%s, run_name='__main__')\n", strconv.Quote(generated), strconv.Quote(driver), strconv.Quote(manifest), strconv.Quote(surface), strconv.Quote(outdir), strconv.Quote(driver))
			run(t, dir, "driver_probe.py", map[string][]byte{"driver_probe.py": []byte(script)})
			if surface == "cook-write" {
				for order, suffix := range []string{"", "-be"} {
					got, err := os.ReadFile(filepath.Join(outdir, "value"+suffix))
					if err != nil || string(got) != string(oracle.cooks["full"][order]) {
						t.Errorf("%s cook-write %s: mismatch %v", pin, suffix, err)
					}
				}
				continue
			}
			if surface == "cook" {
				got, err := os.ReadFile(filepath.Join(outdir, "example"))
				if err != nil || !strings.Contains(string(got), "node 0 Root @0\n") || !strings.Contains(string(got), "leaf.label = \"caf\\xc3\\xa9\" len=5\n") || !strings.Contains(string(got), "counted#count = 1\n") {
					t.Errorf("%s cook: got %q: %v", pin, got, err)
				}
				continue
			}
			expected := map[string]string{"example": "refuse\n"}
			if surface == "cook-forgery" {
				expected = map[string]string{"valid": "open\n", "damaged": "refuse\n"}
			}
			for name, want := range expected {
				got, err := os.ReadFile(filepath.Join(outdir, name))
				if err != nil || string(got) != want {
					t.Errorf("%s %s %s: got %q: %v", pin, surface, name, got, err)
				}
			}
		}
	}
	for _, control := range []struct{ surface, file, before, after, output, want string }{
		{"cook-foreign", "_blocks.py", "return source[0:8][::-1] + source[8:]", "return source", "example", "refuse\n"},
		{"cook-forgery", "_cooks.py", "return cook.open_cook(cls, data, base_offset)", "return None", "valid", "open\n"},
		{"cook", "cook.py", "write_bytes(outdir, name, cook.dump_cook(handle).encode(\"utf-8\"))", "write_bytes(outdir, name, b\"\")", "example", "node 0 Root @0\n"},
		{"cook-write", "cook-write.py", "write_bytes(outdir, name + suffix, result)", "write_bytes(outdir, name + suffix, result[:-1])", "value", string(oracle.cooks["full"][0])},
	} {
		allCookPinss := cookPins()
		if len(allCookPinss) == 0 {
			t.Fatal("cookPins() is empty: the loop below would check nothing")
		}
		for pin, run := range allCookPinss {
			name := control.surface + "-" + pin
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				copyDir := t.TempDir()
				surfaces := filepath.Join(copyDir, "surfaces")
				if err := os.Mkdir(surfaces, 0700); err != nil {
					t.Fatal(err)
				}
				originalDir := filepath.Dir(driver)
				mainBytes, err := os.ReadFile(driver)
				if err != nil {
					t.Fatal(err)
				}
				copyDriver := filepath.Join(copyDir, "main.py")
				if err := os.WriteFile(copyDriver, mainBytes, 0600); err != nil {
					t.Fatal(err)
				}
				for _, file := range []string{"_common.py", "_blocks.py", "_cooks.py", "cook.py", "cook-foreign.py", "cook-forgery.py", "cook-write.py"} {
					raw, err := os.ReadFile(filepath.Join(originalDir, "surfaces", file))
					if err != nil {
						t.Fatal(err)
					}
					if file == control.file {
						if !strings.Contains(string(raw), control.before) {
							t.Fatalf("control anchor absent for %s", control.surface)
						}
						raw = []byte(strings.Replace(string(raw), control.before, control.after, 1))
					}
					if err := os.WriteFile(filepath.Join(surfaces, file), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				outdir := filepath.Join(copyDir, "out")
				if err := os.Mkdir(outdir, 0700); err != nil {
					t.Fatal(err)
				}
				script := fmt.Sprintf("import os, runpy, sys\nos.environ['PYTHON_CONFORMANCE_GENERATED'] = %s\nsys.argv = [%s, %s, %s, %s]\nrunpy.run_path(%s, run_name='__main__')\n", strconv.Quote(generated), strconv.Quote(copyDriver), strconv.Quote(manifest), strconv.Quote(control.surface), strconv.Quote(outdir), strconv.Quote(copyDriver))
				run(t, copyDir, "driver_probe.py", map[string][]byte{"driver_probe.py": []byte(script)})
				got, err := os.ReadFile(filepath.Join(outdir, control.output))
				if err != nil {
					t.Fatal(err)
				}
				if (control.surface == "cook" && strings.Contains(string(got), control.want)) || (control.surface != "cook" && string(got) == control.want) {
					t.Fatalf("negative control failed: %s", control.surface)
				}
			})
		}
	}

}
