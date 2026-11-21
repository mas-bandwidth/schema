package ctable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func cRetainHashZeroProbe(t *testing.T, sabotage bool) (string, error) {
	t.Helper()
	u := unitFrom(t, "package probe\ntable Root { value int32 }\n")
	model := tabletext.NewModel(u)
	cases := []struct {
		name string
		body []byte
	}{
		{"enum", []byte{1, 30, 2, 0}},
		{"union", []byte{1, 15, 2, 6, 1, 7, 0}},
		{"body-field", []byte{1, 13, 4, 2, 6, 7, 0, 0}},
		{"keyed-slot", []byte{1, 16, 5, 6, 1, 2, 1, 7, 0}},
		{"enum-array-and-None", []byte{1, 14, 4, 30, 2, 2, 0, 0}},
		{"union-array-and-None", []byte{1, 14, 7, 15, 2, 2, 6, 1, 7, 0, 0}},
	}
	var body strings.Builder
	body.WriteString(`
 static uint8_t region[65536] __attribute__((aligned(64)));
 static uint8_t storebytes[4096]; static TableRetainId ids[32];
 static uint8_t saved[4096]; int failures=0; int64_t used[6][2];
 #define CHECK(c,n) do { if(!(c)) { fprintf(stderr,"FAIL %s line %d\n",n,__LINE__); failures++; } } while(0)
 `)
	for index, tc := range cases {
		for _, zero := range []bool{false, true} {
			inner := ir.TableWireId("future_inner_hash")
			suffix := "nonzero"
			zi := 0
			if zero {
				inner = 0
				suffix = "hash-zero"
				zi = 1
			}
			name := tc.name + "/" + suffix
			wire := append([]byte{1}, tc.body...)
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future_outer_hash"))
			wire = binary.LittleEndian.AppendUint64(wire, inner)
			wire = binary.LittleEndian.AppendUint64(wire, 2)
			instance := model.New(u.Tables["Root"])
			var report tabletext.Report
			ok, err := tablewire.Decode(model, instance, wire, &report)
			if !ok || err != nil || report.Unknown != 1 || report.Refused {
				t.Fatalf("%s Go plain: %v %v %+v", name, ok, err, report)
			}
			var retain tablewire.Retain
			retain.Capacity = 4096
			retain.IdCapacity = 32
			report = tabletext.Report{}
			ok, err = tablewire.DecodeRetain(model, instance, wire, &retain, &report)
			if !ok || err != nil || report.Unknown != 1 || report.Retained != 1 || report.RetainLost != 0 {
				t.Fatalf("%s Go retain: %v %v %+v", name, ok, err, report)
			}
			var sr tabletext.Report
			out, err := tablewire.EncodeRetain(model, instance, &retain, &sr)
			if err != nil || !bytes.Equal(out, wire) || sr.RetainLost != 0 {
				t.Fatalf("%s Go save: %v %+v", name, err, sr)
			}
			fmt.Fprintf(&body, `
 { const uint8_t wire[]={%s}; TableRetain rt; TableReport r,sr; const Root *value; int pass;
   memset(&r,0,sizeof(r)); value=root_load(region,sizeof(region),wire,sizeof(wire),&r);
   CHECK(value!=NULL && !r.refused && r.unknown==1,%q);
   for(pass=0;pass<2;pass++){
    memset(&rt,0,sizeof(rt)); rt.bytes=storebytes; rt.capacity=sizeof(storebytes); rt.ids=ids; rt.id_capacity=32;
    memset(&r,0,sizeof(r)); value=root_load_retain(region,sizeof(region),wire,sizeof(wire),&rt,&r);
    CHECK(value!=NULL && !r.refused && r.unknown==1 && r.retained==1 && r.retain_lost==0,%q);
    used[%d][%d]=rt.used;
    if(value){ int64_t n=root_measure_retain(value,&rt); memset(&sr,0,sizeof(sr));
      int64_t wrote=root_save_retain(value,&rt,saved,sizeof(saved),&sr);
      CHECK(n==(int64_t)sizeof(wire) && wrote==n && !sr.refused && sr.retain_lost==0 && memcmp(saved,wire,sizeof(wire))==0,%q);
    }
   }
 }
 `, cBytes(wire), name, name, index, zi, name)
		}
		fmt.Fprintf(&body, " CHECK(used[%d][0]==used[%d][1], %q);\n", index, index, tc.name+"/equal-storage")
	}
	body.WriteString("return failures ? 1 : 0;\n")
	var mutate func(map[string][]byte) error
	if sabotage {
		mutate = func(files map[string][]byte) error {
			const anchor = "table_retain_put64(b,id==0?UINT64_MAX:id)"
			src := string(files["ProbeTable.h"])
			if strings.Count(src, anchor) != 1 {
				return fmt.Errorf("control anchor count %d", strings.Count(src, anchor))
			}
			files["ProbeTable.h"] = []byte(strings.Replace(src, anchor, "table_retain_put64(b,id)", 1))
			return nil
		}
	}
	return runCNodeRecordTerminatorProbe(t, u, body.String(), mutate)
}
func TestCRetainNestedHashZero(t *testing.T) {
	slowtest.Gate(t, "generated C retained hash zero and sanitizers")
	out, err := cRetainHashZeroProbe(t, false)
	if err != nil {
		t.Fatalf("retained hash zero: %v\n%s", err, out)
	}
}
func TestCRetainNestedHashZeroNegativeControl(t *testing.T) {
	slowtest.Gate(t, "generated C retained hash zero negative control")
	out, err := cRetainHashZeroProbe(t, true)
	if err == nil {
		t.Fatalf("hash-zero sabotage passed: %s", out)
	}
	for _, name := range []string{"enum", "union", "body-field", "keyed-slot", "enum-array-and-None", "union-array-and-None"} {
		if !strings.Contains(out, "FAIL "+name+"/hash-zero") || strings.Contains(out, "FAIL "+name+"/nonzero") {
			t.Errorf("control did not isolate %s: %s", name, out)
		}
	}
}
