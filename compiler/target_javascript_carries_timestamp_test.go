package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryTimestamp) }

func TestJavaScriptCarriesTimestamp(t *testing.T) {
	for _, prefix := range []string{"", "fixed "} {
		u := unitFromSource(t, "package probe\n"+prefix+"table Clock { at timestamp }\n")
		files, err := New().Generate(u, "js", nil)
		if err != nil || len(files) == 0 {
			t.Fatalf("public js timestamp generation: files=%d err=%v", len(files), err)
		}
	}
	if !carries("js", carryTimestamp) {
		t.Fatal("missing js timestamp registration")
	}
}
