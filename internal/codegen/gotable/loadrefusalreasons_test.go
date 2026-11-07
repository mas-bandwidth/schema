package gotable

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestLoadRefusalReasonPins holds the Go leg's generated reader to the pins
// in internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, the
// table the Goose load is held to by the same rows (goosetable's
// TestGooseLoadRefusalReasons): for each file, <T>ReadStrict's one reason,
// the form refusal TableReport.Reason names, and, for the measure unit,
// <T>LoadMeasureReason over the same bytes (docs/SPEC-TABLES.md §6.5). A
// count-cap row also leaves the builder load's report with no counter moved
// and malformed unset (§2.9). The float rows hold a NaN and an over-cap count
// in both read orders: the first refusal met names the read.
func TestLoadRefusalReasonPins(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]*strings.Builder{"measure": {}, "fixed": {}, "float": {}}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 || rows[f[1]] == nil {
			t.Fatalf("a malformed pin row: %q", line)
		}
		if f[2] == "-" {
			f[2] = ""
		}
		fmt.Fprintf(rows[f[1]], "{%q,%q,%q,%q,%q},\n", f[0], f[2], f[3], f[4], f[5])
	}
	const check = `
 type pin struct{name,wire,strict,form,measure string}
 func member(err error)string{if err==nil{return "ok"};name,_,_:=strings.Cut(err.Error(),":");return name}
 func wire(t *testing.T,h string)[]byte{b,err:=hex.DecodeString(h);if err!=nil{t.Fatal(err)};return b}
 `
	for _, leg := range []struct{ schema, name, read string }{
		{"package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n", "measure",
			"var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};var rep TableReport;RootLoadBuilderTolerant(&b,w,&rep);b.Shutdown();_,mr:=RootLoadMeasureReason(w);measure:=member(mr);" +
				// the builder's count-cap refusal moves no counter and is not damage (§2.9)
				"if p.strict==\"count_over_extent_cap\"&&(rep.Widened!=0||rep.Clamped!=0||rep.KindMismatch!=0||rep.Unknown!=0||rep.Duplicate!=0||rep.Malformed){t.Errorf(\"%s: the count-cap refusal left %+v\",p.name,rep)}"},
		{"package probe\n table Root { f float32\n values []int32 }\n", "float",
			"var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};var rep TableReport;RootLoadBuilderTolerant(&b,w,&rep);b.Shutdown();_,mr:=RootLoadMeasureReason(w);measure:=member(mr);" +
				// two refusals in one read: the first in read order names the
				// builder load too (§2.9), and the count-cap refusal moves no counter
				"first:=\"ok\";if strings.HasPrefix(rep.Reason,\"non_finite_float\"){first=\"non_finite_float\"}else if rep.Reason==\"count_over_extent_cap\"{first=rep.Reason};if first!=p.strict{t.Errorf(\"%s: the builder load names %s (%+v), the pin %s\",p.name,first,rep,p.strict)};" +
				"if p.strict==\"count_over_extent_cap\"&&(rep.Widened!=0||rep.Clamped!=0||rep.KindMismatch!=0||rep.Unknown!=0||rep.Duplicate!=0||rep.Malformed){t.Errorf(\"%s: the count-cap refusal left %+v\",p.name,rep)}"},
		{"package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n", "fixed",
			"var v Root;var rep TableReport;RootLoadTolerant(&v,w,&rep);measure:=\"-\""},
	} {
		t.Run(leg.name, func(t *testing.T) {
			if rows[leg.name].Len() == 0 {
				t.Fatalf("no %s rows", leg.name)
			}
			runGenerated(t, leg.schema, `package probe
 import("encoding/hex";"strings";"testing")
 `+check+`
 func TestPins(t *testing.T){
 for _,p:=range []pin{
 `+rows[leg.name].String()+`}{
 w:=wire(t,p.wire)
 `+leg.read+`
 _,err:=RootReadStrict(w)
 form:=rep.Reason;if p.form=="-"{form="-"}
 if p.measure=="-"{measure="-"}
 if member(err)!=p.strict||form!=p.form||measure!=p.measure{t.Errorf("%s: strict %s, form %s, measure %s; the pin %s, %s, %s",p.name,member(err),form,measure,p.strict,p.form,p.measure)}
 }}
 `)
		})
	}
}
