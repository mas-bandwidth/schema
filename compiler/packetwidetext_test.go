package compiler

import (
	"strings"
	"testing"
)

var packetWidePorts = []string{"c", "rust", "go", "cs", "java", "js", "dart"}

func TestPacketWideTextBesideUnrelatedTable(t *testing.T) {
	u := unitFromSource(t, "package wide\ntype Text { name wstring(4) }\ntable Counter { number int32 }\n")
	for _, target := range packetWidePorts {
		if _, err := New().Generate(u, target, nil); err != nil {
			t.Errorf("%s: unrelated table refused packet-wide text: %v", target, err)
		}
	}
}

func TestPacketWideTextRefusesTableClosure(t *testing.T) {
	for _, edge := range []string{
		"table Root { item Text }",
		"table Root { items [2]Text }",
		"union Choice { item Text }\ntable Root { choice Choice }",
		"table Text { name wstring(4) }\ntable Root { item *Text }",
		"union Choice { item wstring(4) }\ntable Root { choice Choice }",
	} {
		decl := "type Text { name wstring(4) }\n"
		if strings.HasPrefix(edge, "table Text") {
			decl = ""
		}
		u := unitFromSource(t, "package wide\n"+decl+edge+"\n")
		for _, target := range packetWidePorts {
			_, err := New().Generate(u, target, nil)
			if err == nil || !strings.Contains(err.Error(), "table closure") || !strings.Contains(err.Error(), "wstring(N)") {
				t.Errorf("%s failed to refuse the table-wide closure: %v", target, err)
			}
		}
	}
}
