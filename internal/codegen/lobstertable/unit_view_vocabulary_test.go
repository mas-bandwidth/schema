package lobstertable_test

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLobsterUnitViewVocabulary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	source := `package vocabprobe
 enum Mode { Ready }
 enum Unused { Idle }
 flags Options { Fast }
 type Payload { n int32 }
 union Choice { one Payload }
 union UnusedChoice { other Payload }
 table Leaf { n int32 }
 union TableChoice { leaf Leaf }
 table Root {
 mode Mode
 choice Choice
 table_choice TableChoice
 }
 `
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	program := `import .vocabprobeView
let v = vocabprobe.UnitView()
for(v.enums) e:
    for(e.variants) x:
        print("enum " + e.name + " " + x.name + " " + string(x.id))
for(v.flags) e:
    for(e.variants) x:
        print("flags " + e.name + " " + x.name + " " + string(x.id))
for(v.unions) e:
    for(e.variants) x:
        print("union " + e.name + " " + x.name + " " + string(x.id))
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`enum Mode None 0
enum Mode Ready %d
enum Unused None 0
enum Unused Idle 0
flags Options Fast 0
union Choice None 0
union Choice one %d
union TableChoice None 0
union TableChoice leaf %d
union UnusedChoice None 0
union UnusedChoice other 0`, int64(ir.TableWireId("Ready")), int64(ir.TableWireId("one")), int64(ir.TableWireId("leaf")))
	run := func() string { return strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))) }
	if got := run(); got != want {
		t.Fatalf("vocabulary:\n%s\nwant:\n%s", got, want)
	}
	// The gate must detect a missing tag-zero row, not merely check totals.
	module := filepath.Join(g.dir, "ProbeTable.lobster")
	raw, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	const none = `schema_rt.ViewVariant { name: "None", id: 0 }, `
	if !strings.Contains(string(raw), none) {
		t.Fatal("missing None mutation anchor")
	}
	if err := os.WriteFile(module, []byte(strings.Replace(string(raw), none, "", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if run() == want {
		t.Fatal("removing None escaped vocabulary gate")
	}
}
