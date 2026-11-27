package ctable

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
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

// §22.6 explicitly elides compressed floats BEFORE quantization. These are
// lawful writer values even though their decoded grid values equal defaults.
func TestCMessageQuantizedDefaultProjection(t *testing.T) {
	const schema = `package probe
 fixed table Node { sample float32 | min = 0, max = 1, resolution = 0.1 }
 fixed table Root {
 direct float32 | min = 0, max = 1, resolution = 0.1
 child Node
 samples [2]float32 | min = 0, max = 1, resolution = 0.1
 }
 `
	const source = `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %d: %s\n",__LINE__,#x);return 1;}}while(0)
int main(void){
 TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};
 CHECK(announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report));
 Root value;root_reset(&value);value.direct=0.001f;value.child.sample=0.001f;value.samples[0]=0.001f;
 uint8_t wire[128];int64_t n=root_save_messages(&value,1,wire,sizeof(wire),&report),count=1;
 CHECK(n>3 && root_load_messages(&value,&count,&v,wire,n,&report));
 CHECK(count==1 && !report.refused && value.direct==0 && value.child.sample==0 && value.samples[0]==0);
 return 0;
}`
	runCGenerated(t, schema, source)
	runCGenerated(t, schema, source, "-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all")
}

// A sender may claim our build yet carry another complete shape. The raw
// entry proof, including a nested child's shape, must prevent projection of
// that sender's lawful value from becoming a false declared-default refusal.
func TestCMessageDefaultChangedShape(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint("nested-", nested), func(t *testing.T) {
			own := `package probe
 fixed table Root { number uint32 = 7 }
 `
			sender := `package probe
 fixed table Root { number uint32 = 8 | min = 7, max = 8 }
 `
			expr := "value.number"
			if nested {
				own = "package probe\nfixed table Node { number uint32 = 7 }\nfixed table Root { child Node }\n"
				sender = "package probe\nfixed table Node { number uint32 = 8 | min = 7, max = 8 }\nfixed table Root { child Node }\n"
				expr = "value.child.number"
			}
			// This sender tuple is another local record's shape for the same
			// field id: an any-local-match boolean would be insufficient.
			own += "\nfixed table Other { number uint32 = 8 | min = 7, max = 8 }\n"
			u, s := unitFrom(t, own), unitFrom(t, sender)
			ann := tablewire.Announce(s)
			var from, to [8]byte
			binary.LittleEndian.PutUint64(from[:], ir.BuildVersion(s))
			binary.LittleEndian.PutUint64(to[:], ir.BuildVersion(u))
			if bytes.Count(ann, from[:]) != 1 {
				t.Fatal("announcement build version is not unique")
			}
			ann = bytes.Replace(ann, from[:], to[:], 1)
			m := tabletext.NewModel(s)
			inst := m.New(m.Lookup("Root"))
			field := &inst.Fields[0]
			if nested {
				field = &field.Cell.Tab.Fields[0]
			}
			field.Cell.U = 7
			wire, err := tablewire.EncodeMessage(m, inst)
			if err != nil {
				t.Fatal(err)
			}
			source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
static const uint8_t ann[]={%s},wire[]={%s};
int main(void){TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};Root value;int64_t count=1;
 if(!announce_read(&v,ann,sizeof(ann),&report)||!root_load_messages(&value,&count,&v,wire,sizeof(wire),&report)||count!=1||report.refused||%s!=7){fprintf(stderr,"changed shape refused\n");return 1;}return 0;}`, cMessageBytes(ann), cMessageBytes(wire), expr)
			runCGenerated(t, own, source)
			runCGenerated(t, own, source, "-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all")
		})
	}
}

func TestCMessageFullWidthFlagsDefaultProof(t *testing.T) {
	var names []string
	for i := range 64 {
		names = append(names, fmt.Sprintf("Bit%d", i))
	}
	schema := "package probe\nflags Features { " + strings.Join(names, ",") + " }\nfixed table Root { mask Features }\n"
	const source = `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %d: %s\n",__LINE__,#x);return 1;}}while(0)
int main(void){TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};Root value;uint8_t wire[128];int64_t count=1;
 CHECK(announce_read(&v,kTableAnnounce,kTableAnnounceBytes,&report));
 root_reset(&value);value.mask=UINT64_C(1)<<63;
 int64_t n=root_save_messages(&value,1,wire,sizeof(wire),&report);
 CHECK(n>0 && root_load_messages(&value,&count,&v,wire,n,&report) && value.mask==(UINT64_C(1)<<63));
 TableBitWriter w=table_bit_writer(wire,sizeof(wire));table_bit_put(&w,2,8);table_bit_put(&w,0,8);table_bit_put(&w,1,kTableMessageRefBitsHere);table_bit_put(&w,0,64);table_bit_put(&w,0,kTableMessageRefBitsHere);table_bit_align_write(&w);count=1;
 CHECK(!root_load_messages(&value,&count,&v,wire,w.bits/8,&report));
 CHECK(count==0 && report.refused && report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED);
 return 0;}`
	runCGenerated(t, schema, source)
	runCGenerated(t, schema, source, "-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all")
}
