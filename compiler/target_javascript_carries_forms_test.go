package compiler

import (
	"strings"
	"testing"
)

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryForms) }

func TestJavaScriptCarriesForms(t *testing.T) {
	u := unitFromSource(t, `package probe
 table WireOnly | forms="wire" { tags map[uint8]int32 }
 fixed table Hidden | forms="wire" { n int32 }
 `)
	files, err := New().Generate(u, "js", nil)
	if err != nil || len(files) == 0 {
		t.Fatalf("public js forms generation: files=%d err=%v", len(files), err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "Cook.js") || strings.HasSuffix(name, "Block.js") {
			t.Errorf("omitted form file %s", name)
		}
		for _, symbol := range []string{"WireOnlyToJson", "WireOnlyFromJson", "WireOnlyCookMeasure", "HiddenCookMeasure", "HiddenBlock"} {
			if strings.Contains(string(data), "export function "+symbol+"(") || strings.Contains(string(data), "export const "+symbol+" =") {
				t.Errorf("omitted form export %s in %s", symbol, name)
			}
		}
	}
	if !carries("js", carryForms) {
		t.Fatal("missing js forms registration")
	}
}
