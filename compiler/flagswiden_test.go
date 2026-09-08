package compiler

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Flags occupy kind 9, including inside arrays. A name on the declaration
// cannot change a kind-pair rule the bytes alone settle (SPEC-TABLES §4).
func TestFlagsElementWidening(t *testing.T) {
	source := `package p
 enum Key { First, Second }
 table Root { arrays [2]uint8
 list []uint8
 keyed [Key]uint8 }
 `
	target := strings.ReplaceAll(source, "uint8", "Caps") + "flags Caps { A, B, C }\n"
	u := unitFromSource(t, source)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	var report tabletext.Report
	if !m.Read(v, []byte(`{"arrays":[1,3],"list":[2,7],"keyed":{"First":1,"Second":2}}`), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	to := unitFromSource(t, target)
	model := tabletext.NewModel(to)
	value := model.New(to.Tables["Root"])
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(model, value, wire, &report); err != nil || !ok || report.Widened != 3 || report.Malformed || report.KindMismatch != 0 {
		t.Fatalf("oracle: %+v %v", report, err)
	}
	files, err := New().Generate(to, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var probe strings.Builder
	probe.WriteString("#include <cstdlib>\n#include \"ProbeTable.h\"\nusing namespace p;\nint main(){const uint8_t wire[]={")
	for _, b := range wire {
		fmt.Fprintf(&probe, "%d,", b)
	}
	probe.WriteString("};auto n=RootLoadMeasure(wire,sizeof(wire));if(n<0)return 1;auto region=(uint8_t*)malloc(n);TableReport report;auto root=RootLoad(region,n,wire,sizeof(wire),&report);if(!root||report.widened!=3||report.kind_mismatch||report.malformed)return 2;if(root->arrays[1]!=3||root->list.count!=2||root->list[1]!=7||root->keyed[Key::Second]!=2)return 3;free(region);return 0;}\n")
	issue710CompileRun(t, dir, "c++", ".cpp", probe.String())
}
