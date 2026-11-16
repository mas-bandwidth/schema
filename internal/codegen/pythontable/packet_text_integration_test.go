package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

const hostedPacketTextSchema = `package hostedtext

type Badge
{
    title string(4)
    blob bytes(4)
    items [2..3]uint16
}

table Live
{
    badge Badge
}

fixed table Fixed
{
    badge Badge
}
`

const hostedPacketTextProbe = pyrun.ImportPrelude + `
from hostedtext import Badge, Fixed, InvalidValue, Live

v = Badge()
v.title = "é"
v.blob = b"\x00\xff"
v.items = [1, 65535]
data = v.write()
assert Badge.read(data).write() == data, "hosted packet round-trip"
assert isinstance(Live().badge, Badge) and isinstance(Fixed().badge, Badge), "table constructors"
for attr, bad in [("title", "\x00"), ("title", "ééé"), ("blob", b"12345"), ("items", [1])]:
    v = Badge()
    setattr(v, attr, bad)
    try:
        v.write()
    except InvalidValue:
        pass
    else:
        raise AssertionError("hosted validation " + attr)
print("ok")
`

func TestPythonHostedPacketText(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, hostedPacketTextSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []struct {
		name string
		run  func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			if got := strings.TrimSpace(string(pin.run(t, dir, "main.py", map[string][]byte{"main.py": []byte(hostedPacketTextProbe)}))); got != "ok" {
				t.Fatal(got)
			}
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("strict mypy: %v\n%s", err, out)
			}
		})
	}
}

func TestPythonHostedPacketTextNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, hostedPacketTextSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	path := u.Package + "/_schema/__init__.py"
	source := string(files[path])
	const marker = "def _validate_string("
	if strings.Count(source, marker) != 1 {
		t.Fatalf("missing or ambiguous hosted string validator %q", marker)
	}
	files[path] = []byte(strings.Replace(source, marker, "def _removed_validate_string(", 1))
	for _, pin := range []struct {
		name string
		exe  func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		t.Run(pin.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			_, stderr, err := pyrun.Try(t, pin.exe(t), dir, "main.py", map[string][]byte{"main.py": []byte(hostedPacketTextProbe)})
			if err == nil || !strings.Contains(stderr, "_validate_string") {
				t.Fatalf("missing helper not refused by name: %v\n%s", err, stderr)
			}
			out, err := pyrun.Analyze(t, dir, u.Package)
			if err == nil || !strings.Contains(out, "_validate_string") {
				t.Fatalf("analyzer missed missing helper: %v\n%s", err, out)
			}
		})
	}
}
