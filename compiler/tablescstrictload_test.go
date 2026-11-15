package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cOctets spells a wire as a C initializer list.
func cOctets(data []byte) string {
	var s strings.Builder
	for _, b := range data {
		fmt.Fprintf(&s, "0x%02x,", b)
	}
	return s.String()
}

// TestCTablePlainLoadIsStrict pins docs/SPEC-TABLES.md §6.5 on the C target:
// THE PLAIN NAME IS THE ONLY READ. Every wire here is one a retired partial
// read would have kept; the plain name refuses it, hands back nothing (a NULL
// root, 0, or a value reset to its defaults), keeps this read's report and
// sets the one typed reason. A clean wire, and one whose only event is a
// widening, are accepted.
func TestCTablePlainLoadIsStrict(t *testing.T) {
	t.Parallel()
	t.Run("value", testCTablePlainValueLoadIsStrict)
	t.Run("region", testCTablePlainRegionLoadIsStrict)
}

// The value-class read of a VARIABLE root and its text read: an out-of-range
// value, a wrong-kind key in the text form, and a truncated wire. The root is
// a variable table because the plain read it names is form 1's; a declared
// fixed root encodes and reads form 3 (docs/SPEC-TABLES.md §3.4), and the C
// leg's fixed-root plain read is its own work.
func testCTablePlainValueLoadIsStrict(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe
table Root {
 low int32 = 1000 | min = 1000, max = 2000
 wide int64
}
`)
	// THE WIRES ARE THE ORACLE'S (internal/tablewire), written by an older
	// build whose `low` had no bounds and whose `wide` was an int32: low =
	// 5000 is OUT OF RANGE on this read, and wide = 7 as an i32 into an i64 is
	// WIDENED, decoded exactly.
	writer := unitFromSource(t, `package probe
table Root {
 low int32
 wide int32
}
`)
	encode := func(low, wide int64) []byte {
		t.Helper()
		model := tabletext.NewModel(writer)
		value := model.New(writer.Tables["Root"])
		var report tabletext.Report
		if !model.Read(value, fmt.Appendf(nil, `{"low":%d,"wide":%d}`, low, wide), &report) {
			t.Fatalf("oracle text read: %+v", report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	clamped := encode(5000, 0)
	widened := encode(1500, 7)
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
#define PLAIN(x) do { if (!(x)) { fprintf(stderr,"THE PLAIN READ ACCEPTED what the strict read refuses: line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t clamped[]={%s};
static const uint8_t widened[]={%s};
static uint8_t region[4096];
/* The plain region read into the one static region, after its measure. */
static const Root * load(const uint8_t * wire, int64_t bytes, TableReport * report) {
 int64_t need=root_load_measure(wire,bytes);
 if (need<=0 || need>(int64_t)sizeof(region)) { need=(int64_t)sizeof(region); }
 return root_load(region,need,wire,bytes,report);
}
int main(void) {
 const Root * root; RootBuilder builder; TableCtx ctx; TableReport report; uint8_t saved[256]; int64_t n;

 /* OUT OF RANGE: the plain read refuses it, hands back no root, keeps this
    read's report, and names the reason. */
 report.unknown=99; /* a caller's stale report is overwritten */
 PLAIN(load(clamped,sizeof(clamped),&report)==NULL);
 CHECK(report.refused && report.refuse_reason==SCHEMA_TABLE_REFUSE_OUT_OF_RANGE && report.unknown==0);
 PLAIN(load(clamped,sizeof(clamped),NULL)==NULL);

 /* A WIDENING alone is not a loss: the plain read accepts it. */
 memset(&report,0,sizeof(report));
 root=load(widened,sizeof(widened),&report);
 CHECK(root!=NULL);
 CHECK(report.widened==1 && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK && root->low==1500 && root->wide==7);

 /* A CLEAN wire round-trips through the plain read. */
 CHECK(root_builder_init(&builder));
 root_builder_root(&builder)->low=1500; root_builder_root(&builder)->wide=-3;
 ctx.arena=&builder.arena;
 n=root_save(&ctx,root_builder_root(&builder),saved,sizeof(saved)); CHECK(n>0);
 root_builder_shutdown(&builder);
 memset(&report,0,sizeof(report));
 root=load(saved,n,&report);
 CHECK(root!=NULL && root->low==1500 && root->wide==-3);
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_OK);

 /* DAMAGE: a truncated wire is wire_damaged on the plain read. */
 memset(&report,0,sizeof(report));
 PLAIN(load(saved,n-3,&report)==NULL);
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED && report.refused);

 /* THE TEXT READ: a key of the wrong JSON type refuses the whole read. */
 CHECK(root_builder_init(&builder));
 PLAIN(!root_from_json(&builder,"{\"low\":1200,\"wide\":\"seven\"}",27,&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH && report.refused);
 CHECK(root_builder_root(&builder)->low==1000 && root_builder_root(&builder)->wide==0);
 root_builder_shutdown(&builder);
 CHECK(root_builder_init(&builder));
 CHECK(root_from_json(&builder,"{\"low\":1200,\"wide\":7}",21,&report) && root_builder_root(&builder)->wide==7);
 root_builder_shutdown(&builder);
 return 0;
}
`, cOctets(clamped), cOctets(widened)))
}

// The variable root's region read and builder read, on the pinned report
// vector map_depth_root_kind (its root's first field header names another
// kind, which the one read refuses whole), and its text
// read on a key of the wrong JSON type. map_depth itself is clean.
func testCTablePlainRegionLoadIsStrict(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "tables", "maps", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema under ../tables/maps: %v", err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/maps: %v", err)
	}
	clean, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "map_depth.bin"))
	if err != nil {
		t.Fatal(err)
	}
	kind, err := os.ReadFile(filepath.Join("..", "testdata", "wire", "tables", "fuzz-vectors", "map_depth_root_kind.bin"))
	if err != nil {
		t.Fatal(err)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include "DepthTable.h"
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
#define PLAIN(x) do { if (!(x)) { fprintf(stderr,"THE PLAIN READ ACCEPTED what the strict read refuses: line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t clean[]={%s};
static const uint8_t kind[]={%s};
int main(void) {
 TableReport report; DepthBuilder builder; const Depth * root; uint8_t * region; int64_t need;
 static const char text[]="{\"past\":\"late\"}";

 /* THE REGION READ refuses it: no root, this read's report, the typed reason. */
 need=depth_load_measure(kind,sizeof(kind)); CHECK(need>0);
 region=(uint8_t *)malloc((size_t)need); CHECK(region!=NULL);
 report.unknown=99; /* a caller's stale report is overwritten */
 root=depth_load(region,need,kind,sizeof(kind),&report);
 PLAIN(root==NULL);
 /* The VERDICT (refused, no root) is the contract; the reason is this leg's
    own answer, pinned here and never compared across ports
    (docs/rulings.json refusal-verdict-only). The C leg's wire-alone checks
    meet this vector before its kind check does. */
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED && report.refused && report.unknown==0);
 PLAIN(depth_load(region,need,kind,sizeof(kind),NULL)==NULL);
 free(region);

 /* the clean wire is accepted by the plain read */
 need=depth_load_measure(clean,sizeof(clean)); CHECK(need>0);
 region=(uint8_t *)malloc((size_t)need); CHECK(region!=NULL);
 memset(&report,0,sizeof(report));
 root=depth_load(region,need,clean,sizeof(clean),&report);
 CHECK(root!=NULL && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK && !report.refused);
 free(region);

 /* THE BUILDER READ, the same refusal, and the same leg's own reason. */
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 PLAIN(!depth_load_builder(&builder,kind,sizeof(kind),&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED && report.refused);
 depth_builder_shutdown(&builder);
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 CHECK(depth_load_builder(&builder,clean,sizeof(clean),&report) && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK);
 depth_builder_shutdown(&builder);

 /* THE TEXT READ into a builder: a key of the wrong JSON type. */
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 PLAIN(!depth_from_json(&builder,text,(int64_t)strlen(text),&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH);
 depth_builder_shutdown(&builder);
 return 0;
}
`, cOctets(clean), cOctets(kind)))
}
