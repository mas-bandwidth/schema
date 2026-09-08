package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"testing"
)

func TestMessageWriter(t *testing.T) {
	schema := `package probe
 enum Grade { Gold, Silver }
 type Nested { n int32 }
 union Choice { number uint128 | overlay
 pair [2]int16
 words string(12)
 ping }
 table Root { yes bool
 small int16 | min = -5, max = 14
 raw uint64
 big uint128
 text string(15)
 blob bytes(9)
 wide wstring(8)
 items [..3]int32
 grades [Grade]Nested
 choice Choice
 optional ?Nested }
 `
	text := `{"yes":true,"small":-3,"raw":18446744073709551615,"big":340282366920938463463374607431768211455,"text":"hi😀","blob":"AAECAw==","wide":"hello","items":[-3,7],"grades":{"Silver":{"n":4}},"choice":{"pair":[5,9]},"optional":{}}`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(text), &report) || !report.Silent() {
		t.Fatalf("text %+v", report)
	}
	expected, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v, v})
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, schema, fmt.Sprintf(`package probe
 import("testing";"bytes")
 func TestMessage(t *testing.T){var v Root;var report TableReport;if !RootFromJson(&v,[]byte(%q),&report)||report!=(TableReport{}){t.Fatalf("json %%+v",report)};values:=[]*Root{&v,&v};want:=[]byte{%s};if RootMeasureMessages(values)!=int64(len(want)){t.Fatalf("measure %%d want %%d",RootMeasureMessages(values),len(want))};out:=make([]byte,len(want));if RootSaveMessages(values,out,&report)!=int64(len(want))||!bytes.Equal(out,want){t.Fatalf("message %%x want %%x",out,want)}
 var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);announcement:=make([]byte,AnnounceMeasure());Announce(announcement);if !AnnounceRead(&vocab,announcement,&report)||!vocab.Announced||vocab.Count!=TableMessageEntriesHere{t.Fatalf("announce %%+v",report)};for i:=range tableMessageEntries{if vocab.Entries[i]!=tableMessageEntries[i]{t.Fatalf("entry %%d: %%+v want %%+v",i,vocab.Entries[i],tableMessageEntries[i])}}
 var decoded [2]Root;if n,ok:=RootLoadMessages(decoded[:],&vocab,out,&report);!ok||n!=2||decoded[0]!=v||decoded[1]!=v {t.Fatalf("load %%d %%v %%+v got %%+v want %%+v",n,ok,report,decoded[0],v)}
 if allocations:=testing.AllocsPerRun(10,func(){RootMeasureMessages(values);RootSaveMessages(values,out,nil);RootLoadMessages(decoded[:],&vocab,out,nil);vocab.Init(storage[:]);AnnounceRead(&vocab,announcement,nil)});allocations!=0{t.Fatalf("allocations %%v",allocations)}
 }
 `, text, byteLiterals(expected)))
}
