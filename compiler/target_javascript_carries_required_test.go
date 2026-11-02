package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryRequired) }

func TestJavaScriptCarriesRequired(t *testing.T) {
	u := unitFromSource(t, `package probe
 table Child { n int32 | required }
 table Root { child Child
 items []Child
 tags map[uint8]Child
 ptr *Child
 }
 `)
	files, err := New().Generate(u, "js", nil)
	if err != nil || len(files) == 0 {
		t.Fatalf("public js required generation: files=%d err=%v", len(files), err)
	}
	if !carries("js", carryRequired) {
		t.Fatal("missing js required registration")
	}
}
