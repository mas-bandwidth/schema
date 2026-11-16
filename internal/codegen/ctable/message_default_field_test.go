package ctable

import (
	"crypto/sha256"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"strings"
	"testing"
)

// The qualification witness carries LoginRequest.region=None explicitly.
// Under this announcement/build, the writer elides that field (§3.3(a)); the
// public reader must refuse the entire batch, including in release builds.
func TestCMessageDeclaredDefaultField(t *testing.T) {
	schema, err := os.ReadFile("../../../tables/backend/Backend.schema")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("../../../testdata/wire/tables/login_full_message.bin")
	if err != nil {
		t.Fatal(err)
	}
	damaged := append([]byte(nil), golden...)
	if len(damaged) != 51 || damaged[49] != 0x4c {
		t.Fatalf("qualification witness seed changed: %x", golden)
	}
	damaged[49] = 0
	if got := fmt.Sprintf("%x", sha256.Sum256(damaged)); got != "1add3297f61ed3b309be8fac8afa8bdf02aa06effd611b3d1aec6e806def1c41" {
		t.Fatalf("qualification witness changed: %s", got)
	}
	source := fmt.Sprintf(cMessageDefaultFieldProbe, cMessageBytes(golden), cMessageBytes(damaged))
	for _, profile := range []struct {
		name  string
		flags []string
	}{
		{"normal", nil},
		{"release", []string{"-DNDEBUG"}},
		{"sanitized", []string{"-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all"}},
	} {
		t.Run(profile.name, func(t *testing.T) { runCGenerated(t, string(schema), source, profile.flags...) })
	}
}

const cMessageDefaultFieldProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} }while(0)
static const uint8_t golden[]={%s};
static const uint8_t damaged[]={%s};
int main(void)
{
 TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};
 CHECK(announce_read(&vocabulary,kTableAnnounce,kTableAnnounceBytes,&report));
 LoginRequest value;
 int64_t count=1;
 CHECK(login_request_load_messages(&value,&count,&vocabulary,golden,sizeof(golden),&report));
 CHECK(count==1 && !report.refused);
 uint8_t again[128];
 CHECK(login_request_save_messages(&value,1,again,sizeof(again),&report)==sizeof(golden));
 CHECK(memcmp(again,golden,sizeof(golden))==0);
 memset(&value,0x7f,sizeof(value));count=1;
 CHECK(!login_request_load_messages(&value,&count,&vocabulary,damaged,sizeof(damaged),&report));
 CHECK(count==0 && report.refused && report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED);
 CHECK(value.player_id==0 && value.session_token_length==0 && value.client_build==0 && value.region==0);
 /* Another build may give an explicit None a non-default meaning (§3.3(a)). */
 vocabulary.build_version^=1;count=1;
 CHECK(login_request_load_messages(&value,&count,&vocabulary,damaged,sizeof(damaged),&report));
 CHECK(count==1 && !report.refused && value.region==0);
 CHECK(login_request_save_messages(&value,1,again,sizeof(again),&report)==50);
 /* An omitted default is the canonical encoding and remains readable. */
 vocabulary.build_version^=1;login_request_reset(&value);
 int64_t n=login_request_save_messages(&value,1,again,sizeof(again),&report);count=1;
 CHECK(n>0 && login_request_load_messages(&value,&count,&vocabulary,again,n,&report));
 CHECK(count==1 && !report.refused);
 return 0;
}
`

// The predicate is shared with the writer, so exercise fields with different
// elision rules and the two places where None is an ordinary present value.
func TestCMessageDefaultFieldKinds(t *testing.T) {
	const schema = `package probe
 enum State { First, Second }
 fixed table Node { id int32 }
 fixed table Root {
 number uint32 = 7
 state State = Second
 text string(8)
 list [..2]int32
 fixeds [2]int32
 child Node
 states [2]State
 optional ?State
 }
 `
	u := unitFrom(t, schema)
	slots := tablewire.MessageVocabularySlots(u)
	var refs []string
	for _, f := range u.Tables["Root"].Fields {
		refs = append(refs, fmt.Sprint(slots[ir.TableFieldEntry(f).Key()]))
	}
	en := u.Enums["State"]
	second := slots[(ir.TableVocabularyEntry{Id: ir.TableWireId(en.VariantWireName(1))}).Key()]
	source := fmt.Sprintf(cMessageDefaultKindsProbe, strings.Join(refs, ","), second)
	runCGenerated(t, schema, source)
	runCGenerated(t, schema, source, "-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all")
}

const cMessageDefaultKindsProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} }while(0)
static const int refs[]={%s};
static int64_t field_wire(uint8_t *wire,int field,int nondefault)
{
 TableBitWriter w=table_bit_writer(wire,256);
 table_bit_put(&w,2,8);table_bit_put(&w,0,8);
 table_bit_put(&w,(uint64_t)refs[field],kTableMessageRefBitsHere);
 switch(field)
 {
  case 0:table_bit_put(&w,nondefault?0:7,32);break;
  case 1:table_bit_put(&w,nondefault?0:%d,kTableMessageRefBitsHere);break;
  case 2:table_bit_put(&w,0,4);table_bit_align_write(&w);break;
  case 3:table_bit_put(&w,0,2);break;
  case 4:table_bit_put(&w,0,32);table_bit_put(&w,0,32);break;
  case 5:table_bit_put(&w,0,kTableMessageRefBitsHere);break;
 }
 table_bit_put(&w,0,kTableMessageRefBitsHere);table_bit_align_write(&w);
 return w.overflow?-1:w.bits/8;
}
int main(void)
{
 TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};uint8_t wire[256];Root value;int field;
 CHECK(announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report));
 for(field=0;field<6;field++)
 {
  int64_t n=field_wire(wire,field,0),count=1;
  CHECK(n>0);
  CHECK(!root_load_messages(&value,&count,&v,wire,n,&report));
  CHECK(count==0 && report.refused && report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED);
  CHECK(value.number==7 && value.state==STATE_SECOND);
  v.build_version^=1;count=1;
  CHECK(root_load_messages(&value,&count,&v,wire,n,&report));
  CHECK(count==1 && !report.refused);
  v.build_version^=1;
 }
 /* Zero/None differ from these fields' specified defaults and must read. */
 for(field=0;field<2;field++)
 {
  int64_t n=field_wire(wire,field,1),count=1;
  CHECK(root_load_messages(&value,&count,&v,wire,n,&report));
  CHECK(count==1 && !report.refused);
 }
 /* None in an array position and a present optional is not a default field. */
 root_reset(&value);value.states[1]=STATE_FIRST;value.optional_present=1;
 int64_t n=root_save_messages(&value,1,wire,sizeof(wire),&report),count=1;
 CHECK(n>0 && root_load_messages(&value,&count,&v,wire,n,&report));
 CHECK(count==1 && !report.refused && value.states[0]==0 && value.states[1]==STATE_FIRST);
 CHECK(value.optional_present && value.optional==0);
 return 0;
}
`
