package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryUnboundedText) }
func TestJavaScriptCarriesUnboundedText(t *testing.T) {
	for _, fields := range []string{"a []string\n b [..2]?string", "a map[string]int32"} {
		u := unitFromSource(t, "package probe\ntable Root {"+fields+"\n}")
		files, err := New().Generate(u, "js", nil)
		if err != nil || len(files) == 0 {
			t.Fatalf("public unbounded-text generation: files=%d err=%v", len(files), err)
		}
	}
	if !carries("js", carryUnboundedText) {
		t.Fatal("missing js unbounded-text registration")
	}
}
