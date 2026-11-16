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
	if !m.Read(value, []byte(`{"scores":[5,-7,0],"flags":[true,false],"items":[{"value":9},{"value":2}]}`), &rep) || rep.Refused {
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

func TestPythonDriverRefusesUnsupportedListElements(t *testing.T) {
	for _, src := range []string{
		"package p\ntable Node { value int32 }\ntable Root { nodes []*Node }\n",
		"package p\nenum E { One, Two }\ntable Root { values []E }\n",
	} {
		_, err := New().Generate(unitFromSource(t, src), "python", nil)
		if err == nil || !strings.Contains(err.Error(), "unbounded array element") {
			t.Errorf("unsupported list was not refused by name: %v", err)
		}
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

func TestPythonDriverRefusesPacketValueDefault(t *testing.T) {
	u := unitFromSource(t, "package packetdefault\ntype Packet {\n name string(8) = \"hi\"\n}\n")
	_, err := New().Generate(u, "python", nil)
	if err == nil || !strings.Contains(err.Error(), "packet-wire defaults") {
		t.Fatalf("packet value default was not refused by name: %v", err)
	}
}

func TestPythonDriverRefusesTextList(t *testing.T) {
	u := unitFromSource(t, "package p\ntable Root {\n names []string(8)\n}\n")
	_, err := New().Generate(u, "python", nil)
	if err == nil || !strings.Contains(err.Error(), "text list") {
		t.Fatalf("text list was not refused by name: %v", err)
	}
}
