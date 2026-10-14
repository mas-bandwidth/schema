package javatable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func javaVariableTextFixture(t *testing.T, schema, root string, inputs ...string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Root.schema"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	var rows []javaVariableFixture
	for i, input := range inputs {
		value := model.New(u.Tables[root])
		report := new(tabletext.Report)
		if !model.Read(value, []byte(input), report) {
			t.Fatalf("reference: %+v", report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(value)
		if err != nil {
			t.Fatal(err)
		}
		wp, jp := filepath.Join(t.TempDir(), "wire"), filepath.Join(t.TempDir(), "json")
		if err := os.WriteFile(wp, wire, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jp, append(text, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, javaVariableFixture{name: root + string(rune('A'+i)), root: root, wire: wp, json: jp})
	}
	javaVariableFixtures(t, dir, rows)
}

func TestJavaVariableOptionalValues(t *testing.T) {
	t.Parallel()
	javaVariableTextFixture(t, `package probe
type Child { value int32 }
table Root {
 n ?int32
 child ?Child
 pair ?[2]uint32
 rows ?[..3]Child
 absent ?[..2]int32
 after int32
}
`, "Root", `{"n":0,"child":{"value":0},"pair":[0,0],"rows":[],"absent":null,"after":17}`, `{"n":null,"child":null,"pair":null,"rows":null,"absent":null,"after":17}`, `{"n":0,"child":{"value":0},"pair":[],"rows":[],"absent":null,"after":17}`, `{"n":7,"child":{"value":8},"pair":[1,2],"rows":[{"value":9}],"absent":[3],"after":17}`)
}

func TestJavaVariableKeyedValues(t *testing.T) {
	t.Parallel()
	javaVariableTextFixture(t, `package probe
enum Axis { X,Y,Z }
type Child { value int32 = 1 }
table Root {
 scalars [Axis]int32
 children [Axis]Child
 after int32
}
`, "Root", `{"scalars":{"Z":7,"X":2},"children":{"Y":{"value":4}},"after":17}`, `{"scalars":{},"children":{},"after":0}`)
}

// General arms previously reached the fixed emitter's scalar adapter and
// produced invalid Java. Their storage must use the same complete field walk
// as a table member, even when the requested public codec is variable form.
func TestJavaFixedGeneralArmPayloads(t *testing.T) {
	t.Parallel()
	schema := `package probe
union Choice {
 word string(8)
 wide wstring(4)
 rows [..3]uint16
}
fixed table T { item Choice }
`
	for _, tc := range []struct {
		name, setter string
		fields       []string
	}{
		{"word", `vals[0].item.type=1;vals[0].item.word[0]=65;vals[0].item.word[1]=66;vals[0].item.wordLength=2;`, []string{"item.type", "item.word", "item.wordLength"}},
		{"wide", `vals[0].item.type=2;vals[0].item.wide[0]=0xd83d;vals[0].item.wide[1]=0xde00;vals[0].item.wideLength=2;`, []string{"item.type", "item.wide", "item.wideLength"}},
		{"rows", `vals[0].item.type=3;vals[0].item.rows[0]=7;vals[0].item.rows[1]=9;vals[0].item.rowsCount=2;`, []string{"item.type", "item.rows", "item.rowsCount"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t08RoundTrip(t, "general_arm_"+tc.name, schema, tc.setter, tc.fields)
		})
	}
}
