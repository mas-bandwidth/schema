package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// These literal audit vectors put the largest signed LEB length after a
// nonzero cursor. Adding the cursor before checking the extent wraps int64.
func TestDartWireLengthOverflow(t *testing.T) {
	slowtest.Gate(t, "dart")
	schemas, err := filepath.Glob("../../../tables/pointers/*.schema")
	if err != nil || len(schemas) == 0 {
		t.Fatalf("pointer fixtures: %v", err)
	}
	u := loadUnit(t, schemas...)
	model := tabletext.NewModel(u)
	def := model.Lookup("Scene")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	for _, tc := range []struct {
		name string
		node bool
	}{
		{"wire_length_overflow", false},
		{"node_table_length_overflow", true},
		{"node_table_negative_length", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../../testdata/wire/tables/fuzz-vectors", tc.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			var report tabletext.Report
			ok, err := tablewire.Decode(model, model.New(def), data, &report)
			if ok || !tablewire.ReadRefusal(err) || !report.Refused {
				t.Fatalf("independent damage contract: ok%v err%v report%+v", ok, err, report)
			}
			if tc.node {
				n, whole, _ := tablewire.FileRegionMeasure(u, def, data)
				if n != 0 || whole {
					t.Fatalf("independent unframed extent: %d whole%v", n, whole)
				}
			}
			var b strings.Builder
			b.WriteString(strings.ReplaceAll(dartClosurePrelude, "ProbeWire.dart", "GraphdemoWire.dart"))
			fmt.Fprintf(&b, "final data=bytes(%q);\n", hex.EncodeToString(data))
			b.WriteString(`
for(var repeat=0;repeat<2;repeat++){
 final r=w.TableWireReport();w.sceneWireLoad(data,r);
 check(r.refused,'plain damaged length');
 final rr=w.TableWireReport();
 final store=w.TableRetain(4096,List.filled(32,0));
 w.sceneWireLoadRetain(data,store,rr);
 check(rr.refused,'retaining damaged length');
 final measure=w.sceneWireMeasure(data);
`)
			if tc.node {
				b.WriteString("check(measure==0,'unframed node-table measure');\n")
			}
			b.WriteString("}\n}\n")
			runFormsProbe(t, files, b.String())
		})
	}
}
