package compiler

import (
	"strings"
	"testing"
)

func TestPacketWideTextIsRefused(t *testing.T) {
	edges := []string{
		"type Text { name wstring(4) }\ntable Counter { number int32 }\n",
		"type Text { name wstring(4) }\ntable Root { item Text }\n",
		"table Text { name wstring(4) }\ntable Root { item *Text }\n",
		"union Choice { item wstring(4) }\ntable Root { choice Choice }\n",
	}
	for _, edge := range edges {
		u := unitFromSource(t, "package wide\n"+edge)
		for _, target := range New().Targets() {
			_, err := New().Generate(u, target, nil)
			if err == nil || !strings.Contains(err.Error(), wstringRetired) || !strings.Contains(err.Error(), "kind 33") {
				t.Errorf("%s: %v", target, err)
			}
		}
	}
}
