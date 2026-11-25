package ci

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// registryMakeRows is every make target a language-free registry row runs,
// each with the row that runs it: a make row's target, a plant row's
// control, and a make-gotest row's target (one make run of the target whose
// Go tests the row reads by name), a make or plant row first. CI runs the
// registry (ci-fast.yml the pr group, schema-nightly.yml the nightly group),
// so a gate the old workflows ran by name is wired when a row runs it; the
// row's tier says which lane.
func registryMakeRows(t *testing.T, root string) map[string]testreg.Test {
	t.Helper()
	reg, err := testreg.Load(filepath.Join(root, filepath.FromSlash(testreg.DefaultPath)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]testreg.Test{}
	gotestRows := map[string]testreg.Test{}
	for _, row := range reg.Tests {
		if row.PerLang() {
			continue
		}
		var in struct{ Target, Make string }
		if json.Unmarshal(row.Inputs, &in) != nil {
			continue
		}
		target := ""
		switch row.Adapter {
		case "make":
			target = in.Target
		case "plant":
			target = in.Make
		case "make-gotest":
			if _, dup := gotestRows[in.Target]; in.Target != "" && !dup {
				gotestRows[in.Target] = row
			}
		}
		if target == "" {
			continue
		}
		if _, dup := out[target]; !dup {
			out[target] = row
		}
	}
	for target, row := range gotestRows {
		if _, ok := out[target]; !ok {
			out[target] = row
		}
	}
	if len(out) == 0 {
		t.Fatal("the registry has no make or plant row: this reads nothing")
	}
	return out
}
