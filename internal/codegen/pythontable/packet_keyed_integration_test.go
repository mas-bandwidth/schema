package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// Functional: the real mixed unit exports table-keyed wrappers, while its
// packet-keyed array retains the packet core's positional list API.
func TestPythonHostedPacketKeyed(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../../tables/examples")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const probe = pyrun.ImportPrelude + `
import tabledemo as package
from tabledemo import ScoreBoard, KeyedConfigTeams, KeyedConfigHulls, HullConfigTurrets
assert not hasattr(package,'ScoreBoardPerTeam')
v=ScoreBoard()
assert type(v.per_team) is list and len(v.per_team)==3
v.per_team=[0,1,100000]
assert ScoreBoard.read(v.write()).per_team==v.per_team
assert len(KeyedConfigTeams())==3 and len(KeyedConfigHulls())==3 and len(HullConfigTurrets())==3
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatalf("mixed keyed import: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, u.Package); err != nil {
			t.Fatalf("mixed keyed strict mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonHostedPacketKeyedNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../../tables/examples")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := u.Package + "/__init__.py"
	files[name] = append(files[name], []byte("\nfrom .Keyed import ScoreBoardPerTeam\n")...)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + "import tabledemo\n")})
		if err == nil || !strings.Contains(stderr, "ScoreBoardPerTeam") {
			t.Fatalf("keyed export sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}

func pythonHostedPacketKeyedTableFiles(t *testing.T) (map[string][]byte, map[string][]byte) {
	t.Helper()
	schema := strings.Replace(packetKeyedSchema, "board Board", "board Board\n direct [Slot]int32", 1)
	u := loadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for name, text := range map[string]string{
		"full":    `{"board":{"per_slot":{"Alpha":7,"Beta":11}}}`,
		"partial": `{"board":{"per_slot":{"Alpha":7}}}`,
		"empty":   `{}`,
	} {
		inputs[name] = pyKeyedEngineWire(t, u, "Holder", text)
	}
	return files, inputs
}

func TestPythonHostedPacketKeyedTableWire(t *testing.T) {
	t.Parallel()
	files, inputs := pythonHostedPacketKeyedTableFiles(t)
	probe := pyrun.ImportPrelude + `from packetkeyed import Holder, InvalidValue
for name,expected in [('full',[7,11]),('partial',[7,0]),('empty',[0,0])]:
 with open(name,'rb') as f:wire=f.read()
 v=Holder.load(wire)
 assert type(v.board.per_slot) is list and v.board.per_slot==expected
 assert hasattr(v.direct,'_values')
 assert v.save()==wire
assert Holder().save()==wire
for bad in ([1],[1,2,3],[-2147483649,0]):
 v=Holder();v.board.per_slot=bad
 try:v.save()
 except InvalidValue:pass
 else:raise AssertionError('packet keyed table validation')
v=Holder();v.direct=[0,0]
try:v.save()
except InvalidValue:pass
else:raise AssertionError('table keyed wrapper validation')
print('ok')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs["main.py"] = []byte(probe)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatalf("packet keyed table-wire: %v\n%s\n%s", err, out, stderr)
		}
		if output, err := pyrun.Analyze(t, dir, "packetkeyed"); err != nil {
			t.Fatalf("packet keyed strict mypy: %v\n%s", err, output)
		}
	}
}

func TestPythonHostedPacketKeyedTableWireNegativeControl(t *testing.T) {
	t.Parallel()
	files, inputs := pythonHostedPacketKeyedTableFiles(t)
	name := "packetkeyed/Core.py"
	const anchor = "keyed_list=True,"
	if strings.Count(string(files[name]), anchor) != 1 {
		t.Fatal("keyed representation control lost its anchor")
	}
	files[name] = []byte(strings.Replace(string(files[name]), anchor, "keyed_list=False,", 1))
	probe := pyrun.ImportPrelude + `from packetkeyed import Holder
with open('full','rb') as f:wire=f.read()
try:Holder.load(wire)
except AttributeError:raise SystemExit('hosted_keyed_table_control')
raise AssertionError('keyed representation sabotage was not detected')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs["main.py"] = []byte(probe)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err == nil || !strings.Contains(stderr, "hosted_keyed_table_control") {
			t.Fatalf("keyed representation sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}
