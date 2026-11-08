package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryIntegrity) }

func TestJavaScriptCarriesIntegrity(t *testing.T) {
	for _, prefix := range []string{"", "fixed "} {
		u := unitFromSource(t, "package probe\n"+prefix+"table Reading | integrity = \"crc32c\" { x int32 }\n")
		files, err := New().Generate(u, "js", nil)
		if err != nil || len(files) == 0 {
			t.Fatalf("public js integrity generation: files=%d err=%v", len(files), err)
		}
	}
	if !carries("js", carryIntegrity) {
		t.Fatal("missing js integrity registration")
	}
}
