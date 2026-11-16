package tablewire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Every writable corpus instance is checked on each form its declaration
// admits. The place helper also applies this fixed-point check to the values
// built by the round-trip tests, before their deliberate wire mutations.
func TestEngineReadsEverythingItWrites(t *testing.T) {
	t.Parallel()
	const root = "../.."
	data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	models := map[string]*tabletext.Model{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "unit" {
			for _, p := range fields[2:] {
				units[fields[1]] = append(units[fields[1]], filepath.Join(root, p))
			}
		}
		if fields[0] != "instance" {
			continue
		}
		// The manifest explicitly names these as read refusals, with no
		// writable value (one blob shared by two slots, §2.5).
		if strings.Contains(line, "wire-refused") {
			continue
		}
		name, key, table := fields[1], fields[2], fields[3]
		m := models[key]
		if m == nil {
			paths, err := compiler.GatherPaths(units[key])
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			m = tabletext.NewModel(u)
			models[key] = m
		}
		inst := m.New(m.Lookup(table))
		if strings.Contains(line, "no-text") {
			wire, err := os.ReadFile(filepath.Join(root, fields[4]))
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := tablewire.DecodeVariable(m, inst, wire, &tabletext.Report{}); !ok || err != nil {
				t.Fatalf("%s corpus: %v", name, err)
			}
		} else {
			text, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/json", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !m.Read(inst, text, &tabletext.Report{}) {
				t.Fatalf("%s corpus text refused", name)
			}
		}
		t.Run(name, func(t *testing.T) { t.Parallel(); assertEngineWrites(t, m, inst) })
	}
	// The Java graph gate's map_twin value: every a is its default zero;
	// extra is nondefault, and the map value is a shared table pointer.
	m := listModel(t, `package maptwin
    table Node {
        a int32 = 0
        left *Node
        lookup map[int32]*Node
        extra int32 = 0
    }`)
	value := place(t, m, "Node", `{"extra":77,"left":{"extra":77,"left":{"extra":77,"lookup":{"1":{"extra":77}}}}}`)
	assertEngineWrites(t, m, value)
}

func assertEngineWrites(t *testing.T, m *tabletext.Model, value *tabletext.Instance) {
	t.Helper()
	wire, err := tablewire.EncodeVariable(m, value)
	if err != nil {
		t.Fatalf("variable write %s: %v", value.Def.Name, err)
	}
	back := m.New(value.Def)
	if ok, err := tablewire.DecodeVariable(m, back, wire, &tabletext.Report{}); !ok || err != nil {
		t.Fatalf("variable writer output %s refused: %v", value.Def.Name, err)
	}
	again, err := tablewire.EncodeVariable(m, back)
	if err != nil || !bytes.Equal(wire, again) {
		t.Fatalf("variable writer output %s is not a fixed point: %v", value.Def.Name, err)
	}
	if ir.VariableTables(m.Unit)[value.Def.Name] {
		return
	}
	wire, err = tablewire.EncodeMessages(m, []*tabletext.Instance{value, value})
	if err != nil {
		t.Fatalf("message write %s: %v", value.Def.Name, err)
	}
	var vocabulary tablewire.Vocabulary
	if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	values := []*tabletext.Instance{m.New(value.Def), m.New(value.Def)}
	if n, ok, err := tablewire.DecodeMessages(m, values, wire, &vocabulary, &tabletext.Report{}); !ok || err != nil || n != 2 {
		t.Fatalf("message writer output %s refused: count=%d %v", value.Def.Name, n, err)
	}
	again, err = tablewire.EncodeMessages(m, values)
	if err != nil || !bytes.Equal(wire, again) {
		t.Fatalf("message writer output %s is not a fixed point: %v", value.Def.Name, err)
	}
}
