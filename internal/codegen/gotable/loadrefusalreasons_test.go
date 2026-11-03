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
// <T>LoadMeasureReason over the same bytes (docs/SPEC-TABLES.md §6.5).
func TestLoadRefusalReasonPins(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]*strings.Builder{"measure": {}, "fixed": {}}
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
			"var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};var rep TableReport;RootLoadBuilderTolerant(&b,w,&rep);b.Shutdown();_,mr:=RootLoadMeasureReason(w);measure:=member(mr)"},
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
