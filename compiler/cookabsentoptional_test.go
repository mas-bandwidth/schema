package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN ABSENT OPTIONAL'S PAYLOAD IS DEAD BYTES: zero on write, whatever the
// element declares as its defaults (docs/SPEC-TABLES.md §7.2; docs/rulings.json
// fixed-dead-bytes-are-zero). A field that is not supplied READS its declared
// default; that is a read rule, and it puts nothing into dead storage. Here the
// element's defaults are all non-zero, the C storage under the cleared present
// flag holds a stale pattern, and both cooks must leave the payload zero and
// agree byte for byte. The uncook still reads the absent field's defaults.
func TestCookAbsentOptionalPayloadZero(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe
type Pose { x float32 = 0.5
 y int16 = -3
 z uint8 = 9 }
fixed table Root {
 tag uint8 = 1
 spawn ?Pose
 home ?Pose
}
`)
	m := tabletext.NewModel(u)
	value := m.New(u.Tables["Root"])
	report := tabletext.Report{}
	if !m.Read(value, []byte(`{"home":{"x":2}}`), &report) || !report.Silent() {
		t.Fatalf("input: %+v", report)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	cooked, _, _, err := New().Cook(u, "Root", wire, CookOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// the fixed cook's body follows the 64-byte header
	fl := ir.RecordLayout(u, u.Tables["Root"]).FieldByName("spawn")
	payload := ir.FieldPieces(u, fl.Field, fl.Offset)[0]
	for at := 64 + payload.Offset; at < 64+payload.Offset+payload.Size; at++ {
		if cooked[at] != 0 {
			t.Fatalf("Go cook: the absent optional's payload holds 0x%02x at byte %d, want zero", cooked[at], at)
		}
	}
	back, err := tablecook.Uncook(m, u.Tables["Root"], cooked)
	if err != nil {
		t.Fatal(err)
	}
	for i := range back.Fields {
		fv := &back.Fields[i]
		if fv.Def.Name != "spawn" {
			continue
		}
		if fv.Present {
			t.Fatal("uncook: the absent optional reads present")
		}
		pose := fv.Cell.Tab
		if pose == nil || len(pose.Fields) != 3 || pose.Fields[0].Cell.F != 0.5 || pose.Fields[1].Cell.I != -3 || pose.Fields[2].Cell.U != 9 {
			t.Fatalf("uncook: the absent optional does not read its declared defaults: %+v", pose)
		}
	}
	var octets strings.Builder
	for _, v := range cooked {
		fmt.Fprintf(&octets, "0x%02x,", v)
	}
	files, err := generateCUnqualified(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t reference[]={` + octets.String() + `};
int main(void) {
 static uint8_t space[4096]; uint8_t * output=(uint8_t *)(((uintptr_t)space+63)&~(uintptr_t)63);
 Root root; const Root * opened; const uint8_t * p; int64_t need; size_t i;
 root_reset(&root);
 /* a value made absent after it was present: the flag cleared, the storage untouched */
 memset(&root.spawn,0xa5,sizeof(root.spawn));root.spawn_present=0;
 root.home_present=1;root.home.x=2.0f;
 need=root_cook_measure(&root);CHECK(need==(int64_t)sizeof(reference));
 CHECK(root_cook(&root,output,need,TableByteOrder_Little));
 for(i=0;i<sizeof(reference);i++) { if(output[i]!=reference[i]) { fprintf(stderr,"byte %d: C 0x%02x, Go 0x%02x\n",(int)i,output[i],reference[i]); return 1; } }
 CHECK(root_cook(&root,output,need,table_cook_byte_order==1 ? TableByteOrder_Little : TableByteOrder_Big));
 opened=root_open(output,need);CHECK(opened!=NULL);
 CHECK(!opened->spawn_present && opened->home_present && opened->home.x==2.0f && opened->home.y==-3 && opened->home.z==9);
 for(p=(const uint8_t *)&opened->spawn;p<(const uint8_t *)&opened->spawn+sizeof(opened->spawn);p++) {CHECK(*p==0);}
 return 0;
}
`
	issue710CompileRun(t, dir, "cc", ".c", source)
}
