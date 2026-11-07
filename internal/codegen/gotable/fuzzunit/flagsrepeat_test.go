package tabledemo_test

import (
	"strings"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGeneratedJSONFlagsRepeatedNameRefused holds docs/SPEC-TABLES.md §16.2 for
// the generated text reader: a flags name twice in one array refuses the read,
// as the engine's does, and two different names read.
func TestGeneratedJSONFlagsRepeatedNameRefused(t *testing.T) {
	for _, c := range []struct {
		text   string
		accept bool
	}{
		{`{"perks":["Shielded","Turbo"]}`, true},
		{`{"perks":["Turbo","Turbo"]}`, false},
		{`{"perks":["Cloaked","Turbo","Cloaked"]}`, false},
	} {
		var v tabledemo.LoadoutConfig
		var r tabledemo.TableReport
		if got := tabledemo.LoadoutConfigFromJSON(&v, []byte(c.text), &r); got != c.accept {
			t.Errorf("%s: accept=%v, want %v (%+v)", c.text, got, c.accept, r)
		}
	}
}

// TestGeneratedJSONFlagsRepeatedNameNegativeControl removes the rule from the
// generated reader and requires the test red on the repeated names.
func TestGeneratedJSONFlagsRepeatedNameNegativeControl(t *testing.T) {
	t.Parallel()
	out, failed := slowtest.RunSabotaged(t, "../../../..", "./internal/codegen/gotable/fuzzunit", "^TestGeneratedJSONFlagsRepeatedNameRefused$",
		slowtest.Sabotage{File: "internal/codegen/gotable/fuzzunit/TabledemoTableJson.go",
			From: "if bits&(uint64(1)<<bit) != 0 {", To: "if false && bits&(uint64(1)<<bit) != 0 {"})
	if !failed || !strings.Contains(out, `{"perks":["Turbo","Turbo"]}: accept=true`) {
		t.Errorf("negative control: the rule is gone and the test did not go red on its own words\n%s", out)
	}
}
