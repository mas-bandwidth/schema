package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4, R17): a
// reader that meets a flags bit at or above its W (the variant count, or K
// under | max = K) refuses the WHOLE load out_of_range, in the variable form
// and the fixed form alike, and the Go writers refuse such a mask in every
// build, so a same-build round trip never writes what its own reader refuses.
// The newer reader reads old data; the older reader never reads new data. The
// variable rows are the engine's file of a newer declaration (one more
// variant), read by the older generated reader. Red on the build that kept the
// bit and wrote it back.
func TestGoFlagsPastWidthRefuse(t *testing.T) {
	t.Parallel()
	const reader = `package probe
flags Caps { Jump, Fly }
table Doc { caps Caps }
fixed table Rec { caps Caps }
`
	m := tabletext.NewModel(unitOf(t, strings.Replace(reader, "Jump, Fly", "Jump, Fly, Swim", 1)))
	var rows strings.Builder
	for _, tc := range []struct {
		text string
		ok   bool
	}{{`["Fly"]`, true}, {`["Swim"]`, false}, {`["Jump", "Swim"]`, false}} {
		v := m.New(m.Lookup("Doc"))
		var report tabletext.Report
		if !m.Read(v, []byte(`{"caps": `+tc.text+`}`), &report) {
			t.Fatal(report)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&rows, "{%#v,%v},", wire, tc.ok)
	}
	t10Run(t, reader, strings.Replace(flagsPastWidthProbe, "ROWS", rows.String(), 1), false)
}

const flagsPastWidthProbe = `package probe
import("bytes";"errors";"testing")
var rows=[]struct{wire []byte;ok bool}{ROWS}
func TestVariableReadHoldsTheMask(t *testing.T){for i,tc:=range rows{
 var b DocBuilder;if !b.Init(){t.Fatal("init")}
 var report TableReport;ok:=DocLoadBuilder(&b,tc.wire,&report);b.Shutdown()
 if ok!=tc.ok{t.Errorf("row %d: admitted=%v want=%v: %+v",i,ok,tc.ok,report)}
 if !ok&&!errors.Is(report.Err(),TableRefuseOutOfRange){t.Errorf("row %d: refused %v, want out_of_range",i,report.Err())}
}}
func TestVariableWriterHoldsTheMask(t *testing.T){var b DocBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot()
 v.Caps=CapsFly;if DocMeasure(v,&b.Arena)<0{t.Fatal("measure refused a mask inside W")}
 v.Caps=CapsFly|4;if DocMeasure(v,&b.Arena)>=0||DocToJSONMeasure(v)>=0{t.Error("measure admitted a mask bit past W")}
 out:=bytes.Repeat([]byte{0xa5},256);before:=append([]byte(nil),out...);if n:=DocSave(v,out,&b.Arena);n>=0||!bytes.Equal(out,before){t.Errorf("save of a mask bit past W answered %d",n)}
}
func TestFixedHoldsTheMask(t *testing.T){
 wire:=make([]byte,RecFixedMeasure(1));if n:=RecFixedSave([]Rec{{Caps:CapsFly}},wire);n!=int64(len(wire)){t.Fatalf("fixed save answered %d",n)}
 back:=make([]Rec,1);var report TableReport;if RecFixedLoad(back,wire,make([]TableFixedEntry,RecFixedPlanMax),&report)!=1||back[0].Caps!=CapsFly{t.Fatalf("a mask inside W: %+v",report)}
 wire[len(wire)-8]|=4;report=TableReport{};if RecFixedLoad(back,wire,make([]TableFixedEntry,RecFixedPlanMax),&report)>=0||!errors.Is(report.Err(),TableRefuseOutOfRange){t.Fatalf("a mask bit past W read: %v %+v",report.Err(),report)}
 if RecFixedSave([]Rec{{Caps:CapsFly|4}},wire)>=0{t.Fatal("fixed save wrote a mask bit past W")}
}
`
