package gotable

import (
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// feedChainMutant is a FILE-form (form byte 1) mutant of the wire-fuzz corpus
// seed stream_chain: `conformance-harness wire-fuzz --seed 24845619678 --n
// 500000`, random pass #116063. It is damaged.
const feedChainMutant = "01010803000000020f030d0a040e02040e04110306010105110200060c1602070a040e03060102051103000707040e03060103000a040e03060102051103000700c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"

// TestGoFileLoadOfDamagedFeedChainIsRefused pins DAMAGE DISCARDS THE WHOLE LOAD
// (docs/SPEC-TABLES.md §4) on the Go region load: internal/tablewire refuses
// the mutant as malformed, and the generated FeedLoadTolerant owes the same
// refusal. Nothing it decoded is compared.
func TestGoFileLoadOfDamagedFeedChainIsRefused(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../../tables/stream/Stream.schema")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString(feedChainMutant)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unitFrom(t, string(source)))
	var report tabletext.Report
	if ok, err := tablewire.DecodeRegion(model, model.New(model.Lookup("Feed")), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("the oracle does not refuse the mutant as damage: ok=%v err=%v %+v", ok, err, report)
	}
	test := fmt.Sprintf(`package streamdemo
import("testing";"unsafe")
func TestFeedChain(t *testing.T){
 wire:=%#v
 n:=FeedLoadMeasure(wire)
 if n<0{return}
 raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 var r TableReport;FeedLoadTolerant(region,wire,&r)
 if !r.Malformed{t.Fatalf("the Go load did not refuse the damaged wire: %%+v",r)}
}
`, wire)
	runGenerated(t, string(source), test)
}
