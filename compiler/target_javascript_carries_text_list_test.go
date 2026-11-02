package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryTextList, carryTextListTotal) }
func TestJavaScriptCarriesTextList(t *testing.T) {
	for _, suffix := range []string{"", " | total = 5"} {
		u := unitFromSource(t, "package probe\ntable Lists { a [..3]string(5)"+suffix+"\n b []?string(5)"+suffix+"\n}\n")
		files, err := New().Generate(u, "js", nil)
		if err != nil || len(files) == 0 {
			t.Fatalf("public js text-list generation: files=%d err=%v", len(files), err)
		}
	}
	if !carries("js", carryTextList) || !carries("js", carryTextListTotal) {
		t.Fatal("missing text-list registration")
	}
}
