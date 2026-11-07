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
// THE PLAIN NAME IS THE STRICT READ, and the tolerant read exists only under
// its own name. Every wire here is one the tolerant read keeps and names in
// its report; the plain name refuses it, hands back nothing (a NULL root, 0,
// or a value reset to its defaults), keeps this read's report and sets the one
// typed reason. A clean wire, and one whose only event is a widening, are
// accepted by both.
//
// THE SABOTAGE CONTROL (make/c.mk tables-c-strict-load-negative-control)
// makes the plain names tolerant in the emitter, and this test must go red on
// "THE PLAIN READ ACCEPTED".
func TestCTablePlainLoadIsStrict(t *testing.T) {
	t.Parallel()
	t.Run("value", testCTablePlainValueLoadIsStrict)
	t.Run("region", testCTablePlainRegionLoadIsStrict)
}

// The fixed-class value read and its text read: a clamp, a kind mismatch in
// the text form, and a truncated wire.
func testCTablePlainValueLoadIsStrict(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe
fixed table Root {
 low int32 = 1000 | min = 1000, max = 2000
 wide int64
}
`)
	// THE WIRES ARE THE ORACLE'S (internal/tablewire), written by an older
	// build whose `low` had no bounds and whose `wide` was an int32: low =
	// 5000 is CLAMPED on this read, and wide = 7 as an i32 into an i64 is
	// WIDENED, decoded exactly.
	writer := unitFromSource(t, `package probe
fixed table Root {
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
int main(void) {
 Root value; TableReport report; uint8_t saved[64]; int64_t n;

 /* A CLAMP: the tolerant read keeps the value it recovered and names the clamp. */
 memset(&report,0,sizeof(report));
 CHECK(root_load_tolerant(&value,clamped,sizeof(clamped),&report));
 CHECK(report.clamped==1 && !report.malformed && !report.refused);
 /* the plain read refuses it, resets the value, keeps the report, names the reason */
 report.unknown=99; /* a caller's stale report is overwritten */
 PLAIN(!root_load(&value,clamped,sizeof(clamped),&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_CLAMPED && report.clamped==1 && report.unknown==0);
 CHECK(value.low==1000 && value.wide==0); /* the declared defaults */
 PLAIN(!root_load(&value,clamped,sizeof(clamped),NULL));

 /* A WIDENING alone is not a loss: both reads accept it. */
 memset(&report,0,sizeof(report));
 CHECK(root_load(&value,widened,sizeof(widened),&report));
 CHECK(report.widened==1 && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK && value.wide==7);

 /* A CLEAN wire round-trips through the plain read. */
 value.low=1500; value.wide=-3; n=root_save(&value,saved,sizeof(saved)); CHECK(n>0);
 memset(&report,0,sizeof(report));
 CHECK(root_load(&value,saved,n,&report) && value.low==1500 && value.wide==-3);
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_OK);

 /* DAMAGE: a truncated wire is wire_damaged on the plain read. */
 memset(&report,0,sizeof(report));
 PLAIN(!root_load(&value,saved,n-3,&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_WIRE_DAMAGED && report.malformed);

 /* THE TEXT READ: a key of the wrong JSON type is skipped and counted. */
 memset(&report,0,sizeof(report));
 CHECK(root_from_json_tolerant(&value,"{\"low\":1200,\"wide\":\"seven\"}",27,&report));
 CHECK(report.kind_mismatch==1 && value.low==1200);
 PLAIN(!root_from_json(&value,"{\"low\":1200,\"wide\":\"seven\"}",27,&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH && report.kind_mismatch==1);
 CHECK(value.low==1000 && value.wide==0);
 CHECK(root_from_json(&value,"{\"low\":1200,\"wide\":7}",21,&report) && value.wide==7);
 return 0;
}
`, cOctets(clamped), cOctets(widened)))
}

// The variable root's region read and builder read, on the pinned report
// vector map_depth_root_kind (its root's first field header names another
// kind: kind_mismatch 1, and the tolerant read keeps the root), and its text
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

 /* THE REGION READ. The tolerant read keeps the root and names the mismatch. */
 need=depth_load_measure(kind,sizeof(kind)); CHECK(need>0);
 region=(uint8_t *)malloc((size_t)need); CHECK(region!=NULL);
 memset(&report,0,sizeof(report));
 root=depth_load_tolerant(region,need,kind,sizeof(kind),&report);
 CHECK(root!=NULL && report.kind_mismatch==1 && !report.malformed && !report.refused);
 /* the plain read refuses it: no root, this read's report, the typed reason */
 report.clamped=7;
 root=depth_load(region,need,kind,sizeof(kind),&report);
 PLAIN(root==NULL);
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH && report.kind_mismatch==1 && report.clamped==0);
 PLAIN(depth_load(region,need,kind,sizeof(kind),NULL)==NULL);
 free(region);

 /* the clean wire is accepted by the plain read */
 need=depth_load_measure(clean,sizeof(clean)); CHECK(need>0);
 region=(uint8_t *)malloc((size_t)need); CHECK(region!=NULL);
 memset(&report,0,sizeof(report));
 root=depth_load(region,need,clean,sizeof(clean),&report);
 CHECK(root!=NULL && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK && !report.kind_mismatch);
 free(region);

 /* THE BUILDER READ, the same pair. */
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 CHECK(depth_load_builder_tolerant(&builder,kind,sizeof(kind),&report) && report.kind_mismatch==1);
 depth_builder_shutdown(&builder);
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 PLAIN(!depth_load_builder(&builder,kind,sizeof(kind),&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH && report.kind_mismatch==1);
 depth_builder_shutdown(&builder);
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 CHECK(depth_load_builder(&builder,clean,sizeof(clean),&report) && report.refuse_reason==SCHEMA_TABLE_REFUSE_OK);
 depth_builder_shutdown(&builder);

 /* THE TEXT READ into a builder: a key of the wrong JSON type. */
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 CHECK(depth_from_json_tolerant(&builder,text,(int64_t)strlen(text),&report) && report.kind_mismatch==1);
 depth_builder_shutdown(&builder);
 CHECK(depth_builder_init(&builder)); memset(&report,0,sizeof(report));
 PLAIN(!depth_from_json(&builder,text,(int64_t)strlen(text),&report));
 CHECK(report.refuse_reason==SCHEMA_TABLE_REFUSE_KIND_MISMATCH);
 depth_builder_shutdown(&builder);
 return 0;
}
`, cOctets(clean), cOctets(kind)))
}
