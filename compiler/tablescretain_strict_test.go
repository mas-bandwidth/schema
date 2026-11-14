package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCTableRetainRefusesWhatLoadRefuses pins docs/SPEC-TABLES.md §4 and §6.6
// on the C variable-root leg: the retaining load refuses the files the plain
// load refuses, an unknown field it keeps is validated exactly as the plain
// read validates one it skips, and a clean unknown field is still retained.
// The Go engine's verdict is the oracle, and all three reads agree on it.
func TestCTableRetainRefusesWhatLoadRefuses(t *testing.T) {
	t.Parallel()
	const old = "package probe\ntable Root { next *Root }\n"
	oldUnit := unitFromSource(t, old)
	oldModel := tabletext.NewModel(oldUnit)

	floatMarker := make([]byte, 8)
	binary.LittleEndian.PutUint64(floatMarker, math.Float64bits(1.5))

	type damagedUnknown struct {
		name     string
		decl     string
		fixture  string
		damage   func([]byte) []byte
		retained bool
	}
	cases := []damagedUnknown{
		{
			name:    "bool byte",
			decl:    "future bool",
			fixture: `{"future":true}`,
			damage:  func(wire []byte) []byte { wire[3] = 2; return wire },
		},
		{
			name:    "ill-formed string",
			decl:    "future string(8)",
			fixture: `{"future":"hello"}`,
			damage: func(wire []byte) []byte {
				wire[bytes.Index(wire, []byte("hello"))] = 0xff
				return wire
			},
		},
		{
			name:    "nan float array",
			decl:    "future [..4]float64",
			fixture: `{"future":[1.5]}`,
			damage: func(wire []byte) []byte {
				binary.LittleEndian.PutUint64(wire[bytes.Index(wire, floatMarker):], 0x7ff8000000000000)
				return wire
			},
		},
		{
			name:    "array count past its elements",
			decl:    "future [..4]float64",
			fixture: `{"future":[1.5]}`,
			damage: func(wire []byte) []byte {
				wire[bytes.Index(wire, floatMarker)-1] = 3
				return wire
			},
		},
		{
			name:    "truncated int32",
			decl:    "future int32",
			fixture: `{"future":7}`,
			damage: func(wire []byte) []byte {
				at := bytes.Index(wire, []byte{7, 0, 0, 0})
				return append(wire[:at+3], wire[at+4:]...)
			},
		},
		{
			name:     "clean unknown",
			decl:     "future uint32",
			fixture:  `{"future":42}`,
			damage:   func(wire []byte) []byte { return wire },
			retained: true,
		},
	}

	var wires, checks strings.Builder
	for i, c := range cases {
		newer := strings.Replace(old, "next *Root", "next *Root\n"+c.decl, 1)
		model := tabletext.NewModel(unitFromSource(t, newer))
		value := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if !model.Read(value, []byte(c.fixture), &report) || !report.Silent() {
			t.Fatalf("%s: fixture: %+v", c.name, report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		wire = c.damage(wire)

		oracle := oldModel.New(oldModel.Lookup("Root"))
		var verdict tabletext.Report
		ok, err := tablewire.Decode(oldModel, oracle, wire, &verdict)
		accepted := ok && err == nil && !verdict.Refused

		store := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
		retaining := oldModel.New(oldModel.Lookup("Root"))
		var retainReport tabletext.Report
		kept, keepErr := tablewire.DecodeRetain(oldModel, retaining, wire, &store, &retainReport)
		kept = kept && keepErr == nil && !retainReport.Refused
		if kept != accepted {
			t.Fatalf("%s: the engine's two reads disagree: plain=%v retaining=%v", c.name, accepted, kept)
		}
		retained := 0
		if c.retained {
			if !kept || retainReport.Retained != 1 {
				t.Fatalf("%s: the clean unknown field is not retained by the engine: ok=%v report=%+v err=%v", c.name, kept, retainReport, keepErr)
			}
			retained = 1
		}
		if accepted {
			checks.WriteString(fmt.Sprintf(" CHECK(case_verdict(w_%d,sizeof(w_%d),%q,1,%d));\n", i, i, c.name, retained))
		} else {
			checks.WriteString(fmt.Sprintf(" CHECK(case_verdict(w_%d,sizeof(w_%d),%q,0,0));\n", i, i, c.name))
		}
		fmt.Fprintf(&wires, "static const uint8_t w_%d[]={%s};\n", i, cOctets(wire))
	}

	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} } while(0)
%s
static int case_verdict(const uint8_t * wire,int64_t bytes,const char * name,int want,int retained)
{
 TableRetain keep;TableRetainId ids[128];TableReport report;const Root * root;uint8_t * region;int64_t need;int plain,kept;
 need=root_load_measure(wire,bytes);
 if(need<=0){plain=0;} else {
  region=(uint8_t *)malloc((size_t)need);if(!region){fprintf(stderr,"%%s: out of memory\n",name);return 0;}
  memset(&report,0,sizeof(report));root=root_load(region,need,wire,bytes,&report);plain=(root!=NULL&&!report.refused);free(region);
 }
 if(plain!=want){fprintf(stderr,"%%s: plain load verdict %%d, want %%d\n",name,plain,want);return 0;}
 if(need<=0){kept=0;} else {
  uint8_t store[8192];memset(&keep,0,sizeof(keep));keep.bytes=store;keep.capacity=sizeof(store);keep.ids=ids;keep.id_capacity=128;
  region=(uint8_t *)malloc((size_t)need);if(!region){fprintf(stderr,"%%s: out of memory\n",name);return 0;}
  memset(&report,0,sizeof(report));
  root=root_load_retain(region,need,wire,bytes,&keep,&report);kept=(root!=NULL&&!report.refused);
  if(!kept&&(keep.count!=0||keep.used!=0||keep.id_used!=0)){fprintf(stderr,"%%s: refused load left retained state\n",name);free(region);return 0;}
  if(kept&&retained&&(report.retained!=1||keep.count!=1)){fprintf(stderr,"%%s: clean unknown was not retained\n",name);free(region);return 0;}
  free(region);
 }
 if(kept!=want){fprintf(stderr,"%%s: load_retain verdict %%d, want %%d\n",name,kept,want);return 0;}
 return 1;
}
int main(void){
%s
 return 0;
}
`, wires.String(), checks.String())
	runCTableWireProbe(t, oldUnit, source)
}
