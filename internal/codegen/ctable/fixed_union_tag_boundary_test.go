package ctable

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

const fixedUnionBoundarySchema = `package probe
union Choice { tiny uint8
 large [4]uint32 }
fixed table Child { prefix uint32
 selected Choice }
fixed table Overlay { padding uint8
 data ?[8]uint8 }
union OtherChoice { small [2]uint32
 wide [8]uint32 }
fixed table Alternate { prefix uint32
 selected OtherChoice }
union Pick { child Child
 overlay Overlay
 scalar uint64
 alternate Alternate }
fixed table Root { pick Pick }
fixed table Envelope { prefix uint32
 nested Pick }
union EnvelopeChoice { item Envelope
 other uint8 }
fixed table DeepRoot { prefix uint32
 nested EnvelopeChoice }
`

// Tag metadata must survive coalescing without interpreting another arm's
// overlapping scalar or optional payload as that tag.
func TestCFixedUnionTagBoundaries(t *testing.T) {
	u := unitFrom(t, fixedUnionBoundarySchema)
	root := u.Tables["Root"]
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, root)
	dead := fixedDeadMetaOf(u, root)
	split, cut := dead.splitUnionTags(plan, guarded)
	if cut < 0 || cut > len(split) {
		t.Fatal("invalid guarded partition")
	}
	var before, after int64
	for _, e := range plan {
		if e.Op == ir.TableFixedOpCopy {
			before += e.Size
		}
	}
	for _, e := range split {
		if e.Op == ir.TableFixedOpCopy {
			after += e.Size
		}
	}
	if before != after {
		t.Fatalf("copy extent changed: %d -> %d", before, after)
	}
	for _, scope := range dead.unions {
		found := false
		for _, e := range split {
			if e.Op == ir.TableFixedOpCopy && e.Src == scope.at && e.Size == scope.info.tagWidth && slices.Equal(e.GuardLinks, scope.guards) {
				found = true
				// The inner tag's source offset also lies inside Overlay.data's
				// optional payload, but that alternative arm is not its scope.
				if len(scope.guards) > 0 && dead.presentOf(e.Src, e.GuardLinks) != -1 {
					t.Fatal("another arm's optional captured the tag")
				}
			}
		}
		if !found {
			t.Fatal("union tag has no independent validation entry")
		}
	}
	// A scalar in the overlapping alternative retains one unchanged copy.
	for _, e := range plan {
		if e.Op == ir.TableFixedOpCopy && len(e.GuardLinks) == 1 && e.GuardLinks[0].Arg == 3 {
			found := false
			for _, p := range split {
				if p.Src == e.Src && p.Dst == e.Dst && p.Size == e.Size && slices.Equal(p.GuardLinks, e.GuardLinks) {
					found = true
				}
			}
			if !found {
				t.Fatal("overlaid scalar was split on another arm's tag")
			}
		}
	}
}

// Nested live arms retain the complete guard pool beyond the two inline lanes.
func TestCFixedUnionTagGuardChain(t *testing.T) {
	u := unitFrom(t, fixedUnionBoundarySchema)
	root := u.Tables["DeepRoot"]
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, root)
	depth := 0
	for _, e := range plan {
		depth = max(depth, len(e.GuardLinks))
	}
	if depth < 3 {
		t.Fatal("control did not reach the guard pool")
	}
	dead := fixedDeadMetaOf(u, root)
	split, _ := dead.splitUnionTags(plan, guarded)
	for _, scope := range dead.unions {
		found := false
		for _, e := range split {
			if e.Op == ir.TableFixedOpCopy && e.Src == scope.at && e.Size == scope.info.tagWidth && slices.Equal(e.GuardLinks, scope.guards) {
				found = true
			}
		}
		if !found {
			t.Fatal("nested guarded tag has no validation boundary")
		}
	}
}

// A multi-byte tag retains its whole declared boundary after a scalar prefix.
func TestCFixedUnionTagBoundaryWidth(t *testing.T) {
	var source strings.Builder
	source.WriteString("package probe\nunion Many {\n")
	for i := range 256 {
		fmt.Fprintf(&source, " a%d uint8\n", i)
	}
	source.WriteString("}\nfixed table Root { prefix uint16\n selected Many }\n")
	u := unitFrom(t, source.String())
	root := u.Tables["Root"]
	plan, guarded, _ := ir.TableFixedBuildPlanPresentDefaultsWithPool(u, root)
	dead := fixedDeadMetaOf(u, root)
	split, _ := dead.splitUnionTags(plan, guarded)
	scope := dead.unions[0]
	if scope.info.tagWidth != 2 {
		t.Fatal("control did not require a two-byte tag")
	}
	for _, e := range split {
		if e.Op == ir.TableFixedOpCopy && e.Src == scope.at && e.Size == 2 {
			return
		}
	}
	t.Fatal("multi-byte tag lost its boundary")
}

// Valid files are produced and consumed by this schema's generated codecs.
// Overlaid arm and optional metadata must preserve every ordinary roundtrip.
func TestCFixedUnionTagRoundTrip(t *testing.T) {
	for _, profile := range []struct {
		name  string
		flags []string
	}{
		{"normal", nil}, {"release", []string{"-DNDEBUG"}}, {"sanitized", []string{"-DNDEBUG", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-fno-sanitize-recover=all"}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			runCGenerated(t, fixedUnionBoundarySchema, cFixedUnionTagRoundTripProbe, profile.flags...)
		})
	}
}

const cFixedUnionTagRoundTripProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do{if(!(x)){fprintf(stderr,"line %d: %s\n",__LINE__,#x);return 1;}}while(0)
static void record_value(Root *v){root_reset(v);v->pick.type=PICK_TYPE_CHILD;v->pick.as.child.prefix=1;v->pick.as.child.selected.type=CHOICE_TYPE_TINY;v->pick.as.child.selected.as.tiny=7;}
int main(void){
 Root value,out[2];uint8_t wire[2048],again[2048];TableReport report={0};
 for(int mode=0;mode<7;mode++){
  root_reset(&value);
  if(mode==1)record_value(&value);
  if(mode==2){record_value(&value);value.pick.as.child.selected.type=CHOICE_TYPE_LARGE;for(int i=0;i<4;i++)value.pick.as.child.selected.as.large[i]=(uint32_t)i+1;}
  if(mode==3){value.pick.type=PICK_TYPE_SCALAR;value.pick.as.scalar=UINT64_C(9876543210);}
  if(mode==4){value.pick.type=PICK_TYPE_OVERLAY;value.pick.as.overlay.data_present=1;value.pick.as.overlay.data[7]=3;}
  if(mode==5){record_value(&value);value.pick.as.child.selected.type=CHOICE_TYPE_NONE;}
  if(mode==6){value.pick.type=PICK_TYPE_ALTERNATE;value.pick.as.alternate.prefix=1;value.pick.as.alternate.selected.type=OTHER_CHOICE_TYPE_SMALL;value.pick.as.alternate.selected.as.small[0]=1;value.pick.as.alternate.selected.as.small[1]=2;}
  int64_t n=root_fixed_save(&value,1,wire,sizeof(wire));CHECK(n>0);root_reset(out);
  CHECK(root_fixed_load(out,1,wire,n,NULL,0,NULL,&report)==1 && !report.refused);
  CHECK(root_fixed_save(out,1,again,sizeof(again))==n && memcmp(wire,again,(size_t)n)==0);
 }
 DeepRoot deep,deep_out;deep_root_reset(&deep);deep.prefix=1;
 deep.nested.type=ENVELOPE_CHOICE_TYPE_ITEM;deep.nested.as.item.prefix=1;deep.nested.as.item.nested.type=PICK_TYPE_CHILD;
 deep.nested.as.item.nested.as.child.prefix=1;deep.nested.as.item.nested.as.child.selected.type=CHOICE_TYPE_TINY;deep.nested.as.item.nested.as.child.selected.as.tiny=7;
 int64_t deep_n=deep_root_fixed_save(&deep,1,wire,sizeof(wire));CHECK(deep_n>0);deep_root_reset(&deep_out);
 CHECK(deep_root_fixed_load(&deep_out,1,wire,deep_n,NULL,0,NULL,&report)==1 && !report.refused);
 CHECK(deep_root_fixed_save(&deep_out,1,again,sizeof(again))==deep_n && memcmp(wire,again,(size_t)deep_n)==0);
 Root values[2];record_value(values);record_value(values+1);
 int64_t n=root_fixed_save(values,2,wire,sizeof(wire));CHECK(n>0);root_reset(out);root_reset(out+1);
 CHECK(root_fixed_load(out,2,wire,n,NULL,0,NULL,&report)==2 && !report.refused);
 CHECK(root_fixed_save(out,2,again,sizeof(again))==n && memcmp(wire,again,(size_t)n)==0);
 return 0;
}
`
