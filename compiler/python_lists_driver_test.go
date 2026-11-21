package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonDriverListSubset(t *testing.T) {
	c := New()
	u := unitFromSource(t, `package driverlist
table Item { value int32 }
table Root {
    scores []int32
    flags []bool
    items []Item
    nodes []*Item
}
`)
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["Root"])
	var rep tabletext.Report
	if !m.Read(value, []byte(`{"scores":[5,-7,0],"flags":[true,false],"items":[{"value":9},{"value":2}],"nodes":[{"value":11},null]}`), &rep) || rep.Refused {
		t.Fatalf("engine input: %+v", rep)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from driverlist import Root
data = open("wire", "rb").read()
value = Root.load(data)
assert value.scores == [5, -7, 0], "scores"
assert value.flags == [True, False], "flags"
assert [item.value for item in value.items] == [9, 2], "items"
assert value.nodes[0].value == 11 and value.nodes[1] is None, "nodes"
assert value.save() == data, "wire"
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "driverlist"); err != nil {
		t.Fatalf("mypy: %s", out)
	}
	path := filepath.Join(dir, "driverlist", "_schema", "kind_array.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const before = "        return items\n"
	if !strings.Contains(string(src), before) {
		t.Fatal("array negative control target missing")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, "        return []\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if err == nil || !strings.Contains(stderr, "AssertionError: scores") {
		t.Fatalf("array negative control did not fail scores: %v\n%s", err, stderr)
	}
}

func TestPythonDriverScalarValueDefaults(t *testing.T) {
	u := unitFromSource(t, `package driverdefaults
flags Caps { Jump, Fly }
table Root {
    name string(8) = "hi"
    data bytes(4) = "ab"
    caps Caps = { Jump, Fly }
}

`)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["Root"])
	var rep tabletext.Report
	if !m.Read(value, []byte(`{}`), &rep) || rep.Refused {
		t.Fatalf("engine input: %+v", rep)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from driverdefaults import Root
data = open("wire", "rb").read()
value = Root.load(data)
assert value.name == "hi", "name"
assert value.data == b"ab", "data"
assert value.caps == 3, "caps"
assert value.save() == data, "wire"
value.name = "bye"
value.data = b"z"
value.caps = 0
again = Root.load(value.save())
assert (again.name, again.data, again.caps) == ("bye", b"z", 0), "nondefaults"
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "driverdefaults"); err != nil {
		t.Fatalf("mypy: %s", out)
	}
	path := filepath.Join(dir, "driverdefaults", "Probe.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const before = "self.name = \"hi\""
	if !strings.Contains(string(src), before) {
		t.Fatal("default negative control target missing")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, "self.name = \"wrong\"", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if err == nil || !strings.Contains(stderr, "AssertionError: name") {
		t.Fatalf("default negative control did not fail name: %v\n%s", err, stderr)
	}
}

// TestPythonDriverCarriesPacketValueDefault holds the driver to SPEC §4.2's
// string, bytes and flags defaults on the packet wire. The driver used to
// refuse them ("packet-wire defaults are ... only today") while the packet core
// emitted them and the roadmap ticked the row; that refusal was the defect.
// The shared fixture test/packet-defaults/Defaults.schema goes through the
// public driver twice: alone (the packet core) and beside an unrelated table
// (the table frame hosting the packet classes). Each time the constructor
// holds the literal's bytes, the flags mask's bits including the unsigned
// 64-bit masks, and the explicit empty defaults, and the §5 zero form stays
// all-zero. Generation and text only: the interpreter run is
// TestPythonPacketDefaults (make packet-defaults-python).
func TestPythonDriverCarriesPacketValueDefault(t *testing.T) {
	src, err := os.ReadFile("../test/packet-defaults/Defaults.schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []struct{ name, src string }{
		{"packet core", string(src)},
		{"hosted beside a table", string(src) + "\ntable Unrelated { number int32 }\n"},
	} {
		t.Run(unit.name, func(t *testing.T) {
			files, err := New().Generate(unitFromSource(t, unit.src), "python", nil)
			if err != nil {
				t.Fatalf("python refuses packet value defaults: %v", err)
			}
			code := string(files["packetdefaults/Probe.py"])
			if code == "" {
				t.Fatalf("no packetdefaults/Probe.py among %d files", len(files))
			}
			for _, want := range []string{
				// Sample: a multi-byte string, a bytes literal with no
				// escapes (four bytes 5c 6e 5c 74), a mask, and empties
				"        self.name = \"é𐀀\"\n        self.token = b\"\\\\n\\\\t\"\n        self.caps = 5\n        self.empty_name = \"\"\n        self.empty_token = b\"\"\n        self.empty_caps = 0\n",
				// its §5 zero form ignores the defaults
				"    def _s_zero(self) -> None:\n        self.name = \"\"\n        self.token = b\"\"\n        self.caps = 0\n",
				// Prefix: a short literal
				"        self.name = \"é\"\n        self.token = b\"\\\\n\"\n",
				// WideMask: bit 63 and the full mask, unsigned
				"        self.high = 9223372036854775808\n        self.all = 18446744073709551615\n",
				// Conditional: a bool default ahead of a branch
				"        self.enabled = True\n",
			} {
				if !strings.Contains(code, want) {
					t.Errorf("generated Probe.py lacks\n%s", want)
				}
			}
		})
	}
}
