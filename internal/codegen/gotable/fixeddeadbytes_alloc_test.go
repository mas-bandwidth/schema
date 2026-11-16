package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedDeadBytesUnionAllocations(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	runGenerated(t, `package probe
 union Choice { small int16
 wide int64 }
 fixed table ManyUnions { choices [65]Choice }
 `, `package probe
 import "testing"
 func TestUnionAllocs(t *testing.T) {
  // AllocsPerRun requires a serial test in this isolated probe process.
  values:=make([]ManyUnions,1)
  ManyUnionsReset(&values[0])
  data:=make([]byte,ManyUnionsFixedMeasure(1))
  if ManyUnionsFixedSave(values,data)!=int64(len(data)) {t.Fatal("save")}
  plan:=make([]TableFixedEntry,ManyUnionsFixedPlanMax)
  var report TableReport
  if ManyUnionsFixedLoad(values,data,plan,&report)!=1 {t.Fatalf("load: %+v",report)}
  allocations:=testing.AllocsPerRun(10,func(){ if ManyUnionsFixedLoad(values,data,plan,&report)!=1 {panic("load")} })
  if allocations!=0 {t.Fatalf("union dead-byte check allocated %v times per load",allocations)}
 }
 `)
}
