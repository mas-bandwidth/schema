package compiler

import (
	"os"
	"testing"
)

func init() { nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryTextList, carryTextListTotal) }
func TestRustCarriesTextLists(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"../testdata/conformance/feature-witnesses/text-lists/Probe.schema", "../testdata/conformance/feature-witnesses/text-list-total/Probe.schema"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files, err := New().Generate(unitFromSource(t, string(source)), "rust", Options{})
		if err != nil || len(files) == 0 {
			t.Fatalf("Rust text lists: %v", err)
		}
	}

}
