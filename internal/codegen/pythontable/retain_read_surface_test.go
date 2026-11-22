package pythontable

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// retainReadSurfaceLoadAnchor is the generated retaining load of a variable
// root; the negative control replaces it so the load answers a default root.
const retainReadSurfaceLoadAnchor = "        return load_table(cls, data, report, retain)\n"

// runRetainReadSurface generates RT1 (sabotaged by plant when non-nil), runs
// the conformance driver's retain surface over every retain row of the shared
// manifest on both pins, and returns each pin's answers by case.
func runRetainReadSurface(t *testing.T, plant func(files map[string][]byte) int) (string, []string, []map[string][]byte) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	unitPaths := map[string]string{"tblrt1": "test/tables/RT1.schema"}
	neededUnits := []string{"tblrt1"}
	seenUnit := map[string]bool{"tblrt1": true}
	var stripped strings.Builder
	var cases []string
	for line := range strings.SplitSeq(string(manifestBytes), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" {
			unitPaths[fields[1]] = fields[2]
		}
		if len(fields) >= 7 && fields[0] == "retain" {
			stripped.WriteString(strings.Join(fields[:7], " ") + "\n")
			cases = append(cases, fields[1])
			if !seenUnit[fields[2]] {
				seenUnit[fields[2]] = true
				neededUnits = append(neededUnits, fields[2])
			}
		}
	}
	generated := t.TempDir()
	if len(neededUnits) == 0 {
		t.Fatal("neededUnits is empty: the loop below would check nothing")
	}
	for _, uname := range neededUnits {
		rel, ok := unitPaths[uname]
		if !ok {
			t.Fatalf("retain row names unknown unit %s", uname)
		}
		files, err := Generate(loadUnitPaths(t, filepath.Join(root, rel)))
		if err != nil {
			t.Fatal(err)
		}
		if uname == "tblrt1" && plant != nil && plant(files) == 0 {
			t.Fatal("retain read surface control anchor absent")
		}
		unitdir := filepath.Join(generated, uname)
		if err := os.MkdirAll(unitdir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeTree(t, unitdir, files)
	}
	if len(cases) == 0 {
		t.Fatal("the manifest has no retain rows")
	}
	manifest := filepath.Join(t.TempDir(), "retain.manifest")
	if err := os.WriteFile(manifest, []byte(stripped.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var answers []map[string][]byte
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		outdir := t.TempDir()
		cmd := testproc.Command(t, pin, "-W", "error", "-I", "-B", filepath.Join(root, "test/conformance/python/main.py"), manifest, "retain", outdir)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PYTHON_CONFORMANCE_GENERATED="+generated)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("retain driver: %v\n%s", err, output)
		}
		got := map[string][]byte{}
		for _, name := range cases {
			data, err := os.ReadFile(filepath.Join(outdir, name))
			if err != nil {
				absent, _ := os.ReadFile(filepath.Join(outdir, name+".absent"))
				t.Fatalf("%s absent: %v %s", name, err, absent)
			}
			got[name] = data
		}
		answers = append(answers, got)
	}
	return root, cases, answers
}

// retainReadSurfaceDiff names the first case whose answer is not "read" and
// the known value at retain-values/<case>.json, or returns "".
func retainReadSurfaceDiff(t *testing.T, root string, cases []string, got map[string][]byte) string {
	t.Helper()
	for _, name := range cases {
		text, ok := bytes.CutPrefix(got[name], []byte("read\n"))
		if !ok {
			return name + ": the answer is not a read: " + string(got[name])
		}
		wantBytes, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/retain-values", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var have, want any
		decode := func(data []byte, into *any) error {
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			return d.Decode(into)
		}
		if err := decode(text, &have); err != nil {
			return name + ": the answer is not JSON: " + err.Error()
		}
		if err := decode(wantBytes, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(have, want) {
			return name + ": retained read value differs from retain-values: got " + string(text) + " want " + string(wantBytes)
		}
	}
	return ""
}

// TestPythonRetainReadSurface answers the conformance retain surface: every
// retain row of the shared manifest, loaded through load_retain at the row's
// two caller capacities on both pinned interpreters, reads and answers the
// complete known value pinned at retain-values/<case>.json (FORMAT.md, retain).
func TestPythonRetainReadSurface(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retain manifest")
	root, cases, answers := runRetainReadSurface(t, nil)
	for _, got := range answers {
		if diff := retainReadSurfaceDiff(t, root, cases, got); diff != "" {
			t.Fatal(diff)
		}
	}
}

// TestPythonRetainReadSurfaceNegativeControl plants a retaining load that
// answers a default root in a copy of the generated unit; the retain surface
// gate must see the lost known value by case name.
func TestPythonRetainReadSurfaceNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the retain manifest")
	root, cases, answers := runRetainReadSurface(t, func(files map[string][]byte) int {
		planted := 0
		for name, data := range files {
			if !strings.HasPrefix(name, "tblrt1/") || strings.Contains(name, "/_schema/") {
				continue
			}
			text := string(data)
			if strings.Contains(text, retainReadSurfaceLoadAnchor) {
				files[name] = []byte(strings.ReplaceAll(text, retainReadSurfaceLoadAnchor, "        load_table(cls, data, report, retain)\n        return cls()\n"))
				planted++
			}
		}
		return planted
	})
	for _, got := range answers {
		diff := retainReadSurfaceDiff(t, root, cases, got)
		if !strings.Contains(diff, "retain_round_trip: retained read value differs") {
			t.Fatalf("planted default-root retaining load was not caught by name: %q", diff)
		}
	}
}
