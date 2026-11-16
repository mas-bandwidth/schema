package pythontable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A historical unsigned integer field can widen into a flags field; the
// reader must count the widening and write the current canonical wire.
func TestPythonAuditWidenFlags(t *testing.T) {
	slowtest.Gate(t, "unsigned historical field to flags")
	old := loadUnit(t, "package flagwiden\ntable Root { value uint8 }\n")
	next := loadUnit(t, "package flagwiden\nflags Caps { A, B, C }\ntable Root { value Caps }\n")
	wire := wideEngineWire(t, old, "Root", `{"value":7}`)
	m := tabletext.NewModel(next)
	v := m.New(m.Lookup("Root"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, v, wire, &r)
	if !ok || err != nil || r.Widened != 1 {
		t.Fatalf("Go oracle %v %v %+v", ok, err, r)
	}
	want, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(next)
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	writeTree(t, d, files)
	probe := pyrun.ImportPrelude + `from flagwiden import Root, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport();v=Root.load(data,r)
assert v.value==7 and r.widened==1
print(v.save().hex())
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, py, d, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || out != fmt.Sprintf("%x\n", want) {
			t.Errorf("%s: %q %s %v", py, out, stderr, err)
		}
	}
}

// The same unsigned-to-flags evolution must work in supported containers.
func TestPythonAuditWidenFlagsContainers(t *testing.T) {
	slowtest.Gate(t, "historical uint8 to flags in containers")
	for _, tc := range []struct{ name, old, new, text, check string }{
		{"array", `package flagcontainer
 table Root { values []uint8 }`, `package flagcontainer
 flags Caps { A, B, C }
 table Root { values []Caps }`, `{"values":[1,7]}`, `v.values == [1,7]`},
		{"keyed", `package flagcontainer
 enum Slot { A, B }
 table Root { values [Slot]uint8 }`, `package flagcontainer
 enum Slot { A, B }
 flags Caps { A, B, C }
 table Root { values [Slot]Caps }`, `{"values":{"A":1,"B":7}}`, `v.values[1] == 1 and v.values[2] == 7`},
		{"union", `package flagcontainer
 union Choice { number uint8 }
 table Root { pick Choice }`, `package flagcontainer
 flags Caps { A, B, C }
 union Choice { number Caps }
 table Root { pick Choice }`, `{"pick":{"number":7}}`, `v.pick.number == 7`},
		{"optional-array", `package flagcontainer
 table Root { values ?[..4]uint8 }`, `package flagcontainer
 flags Caps { A, B, C }
 table Root { values ?[..4]Caps }`, `{"values":[1,7]}`, `v.values == [1,7]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, next := loadUnit(t, tc.old), loadUnit(t, tc.new)
			wire := wideEngineWire(t, old, "Root", tc.text)
			m := tabletext.NewModel(next)
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, v, wire, &r)
			if !ok || err != nil || r.Widened != 1 {
				t.Fatalf("Go oracle %v %v %+v", ok, err, r)
			}
			want, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(next)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`from flagcontainer import Root, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport();v=Root.load(data,r)
assert %s and r.widened==1
print(v.save().hex())
`, tc.check)
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
				if err != nil || out != fmt.Sprintf("%x\n", want) {
					t.Errorf("%s %q %s %v", py, out, stderr, err)
				}
			}
		})
	}
}
