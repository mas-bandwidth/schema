package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const cMessageSchema = `package probe
enum Choice { First, Second }
flags Features { One, Two, Three }
type Leaf { x int32 = -17 | min = -1000, max = 1000
choice Choice }
union Body { text string(16)
numbers [..3]int16
leaf Leaf
empty }
fixed table Root {
 enabled bool
 amount int16 = -3 | min = -8, max = 47
 ratio float32 | min = -1, max = 1, resolution = 0.01
 mask Features
 name string(12)
 payload bytes(12)
 units wstring(12)
 leaf Leaf
 counters [2]uint32
 slots [Choice]Leaf
 samples [..3]int32 | min = 0, max = 0
 body Body
 history [..2]Body
 choices ?[2]Choice
}
`

func TestCTableMessageSave(t *testing.T) {
	u := unitFromSource(t, cMessageSchema)
	m := tabletext.NewModel(u)
	texts := []string{`{}`, `{"enabled":true,"amount":17,"ratio":0.42,"mask":["One","Three"],"name":"hi","payload":"AQID","units":"𝄞","leaf":{"x":9,"choice":"Second"},"counters":[4,5],"slots":{"First":{"x":5}},"samples":[0,0,0],"body":{"text":"hello"},"history":[{"numbers":[2,3]},{"empty":null}],"choices":["First","Second"]}`, `{"body":{"leaf":{"x":22}},"payload":"BAUGBw=="}`}
	var instances []*tabletext.Instance
	var setup strings.Builder
	for i, s := range texts {
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(v, []byte(s), &report) || !report.Silent() {
			t.Fatalf("fixture %d: %+v", i, report)
		}
		instances = append(instances, v)
		fmt.Fprintf(&setup, "CHECK(root_from_json(values+%d,%q,%d,&report));\n", i, s, len(s))
	}
	expected, err := tablewire.EncodeMessages(m, instances)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(b []byte) string {
		var s strings.Builder
		for _, v := range b {
			fmt.Fprintf(&s, "0x%02x,", v)
		}
		return s.String()
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do {if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}} while(0)
static const uint8_t expected[]={%s};
static const uint8_t announcement[]={%s};
int main(void) {
 Root values[3],decoded[3]; TableReport report={0}; TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);uint8_t output[4096];int64_t n,count;
  %s
  CHECK(!report.refused && !report.unknown);
  CHECK(announce_measure()==kTableAnnounceBytes);CHECK(announce(output,sizeof(output))==kTableAnnounceBytes);CHECK(!memcmp(output,kTableAnnounce,kTableAnnounceBytes));
  CHECK(announce_read(&vocabulary,announcement,sizeof(announcement),&report));CHECK(vocabulary.announced && vocabulary.count==%d);
  CHECK(!report.refused);
 n=root_measure_messages(values,3,&report);CHECK(n==sizeof(expected));
 CHECK(root_save_messages(values,3,output,n,&report)==n);CHECK(!memcmp(output,expected,sizeof(expected)));
  count=3;CHECK(root_load_messages(decoded,&count,&vocabulary,expected,sizeof(expected),&report));CHECK(count==3);
  CHECK(!report.refused&&!report.unknown&&!report.widened);
 CHECK(root_save_messages(decoded,3,output,n,&report)==n);CHECK(!memcmp(output,expected,sizeof(expected)));
 output[n-1]=0xa5;CHECK(root_save_messages(values,3,output,n-1,&report)==-1);CHECK(output[n-1]==0xa5);
 CHECK(root_measure_messages(values,0,&report)==-1);CHECK(root_measure_messages(values,257,&report)==-1);
  CHECK(report.refused && report.reason==SCHEMA_TABLE_BATCH_TOO_LARGE);
 return 0;
}`, octets(expected), octets(tablewire.Announce(u)), setup.String(), len(tablewire.MessageVocabulary(u)))
	runCTableWireProbe(t, u, source)
}

func TestCTableAnnouncementDistinctQuantizedShapes(t *testing.T) {
	u := unitFromSource(t, `package probe
fixed table First { rate float32 | min = 0, max = 1, resolution = 0.1 }
fixed table Second { rate float32 | min = 0, max = 1, resolution = 0.1001 }
`)
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatal(err)
	}
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
#include <stdio.h>
int main(void) {
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};uint8_t bytes[kTableAnnounceBytes];
 if(sizeof(TableMessageEntry)!=96)return 2;
 if(announce(bytes,sizeof(bytes))!=sizeof(bytes))return 1;
  if(!announce_read(&v,bytes,sizeof(bytes),&report)){fprintf(stderr,"own announcement refused\n");return 1;}
	return 0;
}`)
}

// A message read across a lawful lineage (docs/SPEC-TABLES.md §4, §6.5):
// every kind the receiver widened decodes exactly and counts `widened`, and
// the fields the receiver dropped count `unknown`. The engine is the oracle
// for the C read, which reports the same counters, nothing past them, and
// saves the same bytes. A receiver whose ranges and bounds the sender's
// values cross, or whose kind changed, refuses the message whole.
func TestCTableMessageEvolution(t *testing.T) {
	sender := unitFromSource(t, `package probe
type Item { value int8 }
union Payload { number uint8 }
fixed table Root { x int8
y uint8
actual float32
name string(16)
values [..4]uint8
child Item
choice Payload
extra uint32
changed bool }
`)
	receiver := unitFromSource(t, `package probe
type Item { value int32 }
union Payload { number uint32 }
fixed table Root { x int32
y uint64
actual float64
name string(16)
values [..4]uint32
child Item
choice Payload }
`)
	sm, rm := tabletext.NewModel(sender), tabletext.NewModel(receiver)
	v := sm.New(sm.Lookup("Root"))
	var report tabletext.Report
	text := []byte(`{"x":-3,"y":200,"actual":1.25,"name":"ééé","values":[4,150,20],"child":{"value":-5},"choice":{"number":255},"extra":7,"changed":true}`)
	if !sm.Read(v, text, &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.EncodeMessage(sm, v)
	if err != nil {
		t.Fatal(err)
	}
	announcement := tablewire.Announce(sender)
	var vocabulary tablewire.Vocabulary
	if err := vocabulary.AnnounceRead(announcement, &report); err != nil {
		t.Fatal(err)
	}

	// the refusal: each receiver below crosses one rule, and the message
	// refuses whole with no value placed
	for _, crossed := range []string{
		"x int32 = 1000 | min=1000,max=2000",
		"y uint64 | min=0,max=100",
		"name string(4)",
		"values [..2]uint32",
		"values [..4]uint32 | min=0,max=100",
		"changed string(4)",
	} {
		field := strings.Fields(crossed)[0]
		var lines []string
		for _, line := range []string{"x int32", "y uint64", "actual float64", "name string(16)", "values [..4]uint32", "child Item", "choice Payload"} {
			if strings.Fields(line)[0] != field {
				lines = append(lines, line)
			}
		}
		lines = append(lines, crossed)
		ru := unitFromSource(t, "package probe\ntype Item { value int32 }\nunion Payload { number uint32 }\nfixed table Root { "+strings.Join(lines, "\n")+" }\n")
		rr := tabletext.NewModel(ru)
		var refused tabletext.Report
		if ok, err := tablewire.DecodeMessage(rr, rr.New(rr.Lookup("Root")), wire, &vocabulary, &refused); ok || !tablewire.ReadRefusal(err) || !refused.Refused {
			t.Fatalf("%s: the message read: ok=%v err=%v %+v", crossed, ok, err, refused)
		}
	}

	report = tabletext.Report{}
	decoded := rm.New(rm.Lookup("Root"))
	if ok, err := tablewire.DecodeMessage(rm, decoded, wire, &vocabulary, &report); err != nil || !ok {
		t.Fatalf("oracle: %v %+v", err, report)
	}
	if report.Refused || report.Widened == 0 || report.Unknown != 2 {
		t.Fatalf("oracle: want widenings and two unknown fields, got %+v", report)
	}
	expected, err := tablewire.Encode(rm, decoded)
	if err != nil {
		t.Fatal(err)
	}
	octets := func(b []byte) string {
		var out strings.Builder
		for _, v := range b {
			fmt.Fprintf(&out, "0x%02x,", v)
		}
		return out.String()
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
static const uint8_t announcement[]={%s},wire[]={%s},expected[]={%s};
int main(void){
 Root value;TableMessageEntry entries[128];TableVocabulary v=table_vocabulary(entries,128);TableReport report={0};int64_t count=1,n;uint8_t saved[4096];
  CHECK(announce_read(&v,announcement,sizeof(announcement),&report));CHECK(root_load_messages(&value,&count,&v,wire,sizeof(wire),&report));
  CHECK(!report.refused&&report.widened==%d&&report.unknown==%d);
  n=root_fixed_save(&value,1,saved,sizeof(saved));CHECK(n==sizeof(expected));CHECK(!memcmp(saved,expected,sizeof(expected)));return 0;
}`, octets(announcement), octets(wire), octets(expected), report.Widened, report.Unknown)
	runCTableWireProbe(t, receiver, source)
}

func TestCTableAnnouncementFailureClearsEntries(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root { a int32\nb string(8)\nc bool }\n")
	runCTableWireProbe(t, u, `#include "ProbeTable.h"
 int main(void){uint8_t wire[2048];TableMessageEntry entries[1];TableVocabulary v;TableReport report={0};int64_t n=announce(wire,sizeof(wire));size_t i;
 if(n<=0){return 1;}memset(entries,0xa5,sizeof(entries));v=table_vocabulary(entries,1);
 if(announce_read(&v,wire,n,&report)||!report.refused||report.reason!=SCHEMA_TABLE_VOCABULARY_TOO_LARGE)return 2;
 for(i=0;i<sizeof(entries);i++)if(((uint8_t*)entries)[i])return 3;
 v=table_vocabulary(NULL,1);memset(&report,0,sizeof(report));if(announce_read(&v,wire,n,&report)||report.reason!=SCHEMA_TABLE_NO_VOCABULARY)return 4;
 {TableMessageEntry full[kTableMessageEntriesHere];int64_t at;size_t j;int failures=0;for(at=0;at<n;at++){memset(full,0,sizeof(full));v=table_vocabulary(full,kTableMessageEntriesHere);report=(TableReport){0};wire[at]^=0x80;
 if(!announce_read(&v,wire,n,&report)){failures++;for(j=0;j<sizeof(full);j++)if(((uint8_t *)full)[j])return 5;}wire[at]^=0x80;}if(!failures)return 6;}return 0;
 }`)
}
