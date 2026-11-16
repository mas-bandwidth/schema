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
