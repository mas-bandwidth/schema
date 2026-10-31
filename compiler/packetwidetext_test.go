package compiler

import (
	"strings"
	"testing"
)

var packetWidePorts = []string{"c", "rust", "go", "cs", "java", "js", "dart", "elixir", "lobster"}

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
			// go and cs carry table wide text on the id-table wire; dart,
			// rust, java, js and elixir carry it on §3.4's fixed form. The
			// refusal under test is the one a port without a table codec
			// gives, and every registered target has one today. lobster
			// carries it on its variable form (carries_lobster_wide_kinds.go).
			if target == "go" || target == "cs" || target == "dart" ||
				target == "rust" || target == "java" || target == "js" || target == "elixir" || target == "lobster" {
				continue
			}
			_, err := New().Generate(u, target, nil)
			if target == "c" {
				if err != nil {
					t.Errorf("C refused table wide text: %v", err)
				}
				continue
			}
			if err == nil || !strings.Contains(err.Error(), "table closure") || !strings.Contains(err.Error(), "wstring(N)") {
				t.Errorf("%s failed to refuse the table-wide closure: %v", target, err)
			}
		}
	}
}
