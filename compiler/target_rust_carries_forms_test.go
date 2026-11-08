package compiler

import (
	"fmt"
	"strings"
	"testing"
)

func init() { nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryForms) }

func TestRustCarriesForms(t *testing.T) {
	t.Parallel()
	for mask := range 8 {
		forms := "wire"
		for bit, name := range []string{"json", "block", "cook"} {
			if mask&(1<<bit) != 0 {
				forms += "," + name
			}
		}
		u := unitFromSource(t, fmt.Sprintf("package probe\nfixed table Root | forms=%q { value int32 }\n", forms))
		files, err := New().Generate(u, "rust", Options{})
		if err != nil {
			t.Fatal(err)
		}
		var source strings.Builder
		for _, b := range files {
			source.Write(b)
		}
		for bit, symbol := range map[int]string{1: "pub fn root_from_json(", 2: "pub struct RootBlock {", 4: "pub struct RootCook {"} {
			if strings.Contains(source.String(), symbol) != (mask&bit != 0) {
				t.Fatalf("mask %d export %s", mask, symbol)
			}
		}
	}
}
