package gotable

import (
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// feedChainMutant is the FILE-form (form byte 1) mutant of the wire-fuzz
// corpus seed stream_chain on which the Go leg kept less than the oracle:
// `conformance-harness wire-fuzz --seed 24845619678 --n 500000`, random pass
// #116063. Both sides reported kind_mismatch 3 and malformed, but the Go save
// was 24 bytes where the oracle's was 46.
const feedChainMutant = "01010803000000020f030d0a040e02040e04110306010105110200060c1602070a040e03060102051103000707040e03060103000a040e03060102051103000700c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"

// TestGoFileLoadOfDamagedFeedChainSavesWhatTheOracleSaves pins §4's
// framing-damage rule on the Go region load (docs/SPEC-TABLES.md §4: the
// decoded prefix stands, the parent reads on past the field's `L`): the
// generated FeedLoad keeps exactly what internal/tablewire keeps of the same
// bytes, so its save is the oracle's encode byte for byte, and the reports
// agree.
func TestGoFileLoadOfDamagedFeedChainSavesWhatTheOracleSaves(t *testing.T) {
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
	value := model.New(model.Lookup("Feed"))
	var report tabletext.Report
	if _, err := tablewire.DecodeRegion(model, value, wire, &report); err != nil {
		t.Fatalf("oracle decode: %v", err)
	}
	want, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatalf("oracle text: %v", err)
	}
	test := fmt.Sprintf(`package streamdemo
import("bytes";"testing";"unsafe")
func TestFeedChain(t *testing.T){
 wire:=%#v;want:=%#v
 n:=FeedLoadMeasure(wire);if n<0{t.Fatalf("load measure %%d",n)}
 raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
 var r TableReport;v:=FeedLoad(region,wire,&r);if v==nil{t.Fatalf("load refused %%+v",r)}
 if int(r.Unknown)!=%d||int(r.KindMismatch)!=%d||int(r.Widened)!=%d||int(r.Clamped)!=%d||int(r.Duplicate)!=%d||r.Malformed!=%t||(r.Verdict==TableOpenRefused)!=%t{t.Fatalf("report %%+v, the oracle's is %s",r)}
 size:=FeedMeasure(v);out:=make([]byte,max(size,0));if size<0||FeedSave(v,out)!=size{t.Fatalf("save %%d",size)}
 if !bytes.Equal(out,want){i:=0;for i<len(out)&&i<len(want)&&out[i]==want[i]{i++};t.Fatalf("the Go load saved %%d bytes where the oracle encodes %%d, first difference at byte %%d\n go     %%x\n oracle %%x\n go     %%s\n oracle %%s",len(out),len(want),i,out,want,feedChainText(v),%q)}
}
func feedChainText(v *Feed) string { n:=FeedToJsonMeasure(v);if n<0{return "?"};b:=make([]byte,n);FeedToJson(v,b);return string(b)
}
`, wire, want, report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, report.Malformed, report.Refused, fmt.Sprintf("%+v", report), text)
	runGenerated(t, string(source), test)
}
