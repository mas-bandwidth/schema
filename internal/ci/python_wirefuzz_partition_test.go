package ci

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The twelve real support rows replace only the expensive omnibus; focused
// pipe and planted control tests must remain in the discovered package shards.
func TestPythonCIWireFuzzPartitionCoverage(t *testing.T) {
	slowtest.Gate(t, "Python wire-fuzz CI plan")
	cmd := exec.Command("sh", "tools/python-ci-rows.sh")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan Python rows: %v\n%s", err, out)
	}
	var rows []struct{ Target, Package, Test string }
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	for _, pin := range []string{"current", "floor"} {
		for _, mode := range []string{"normal", "dev"} {
			for _, arm := range []string{"plain", "mixed", "retain"} {
				for _, shard := range []string{"0", "1"} {
					want["python-wirefuzz-"+pin+"-"+mode+"-"+arm+"-shard-"+shard] = 0
				}
			}
		}
	}
	focused := map[string]bool{"TestPythonWireFuzzFileDispatch": false, "TestPythonWireFuzzVerdictPipeNegativeControl": false}
	for _, row := range rows {
		if _, ok := want[row.Target]; ok {
			want[row.Target]++
		}
		if !strings.HasSuffix(row.Package, "/internal/codegen/pythontable") || row.Test == "" {
			continue
		}
		re, err := regexp.Compile("^(" + row.Test + ")$")
		if err != nil {
			t.Fatal(err)
		}
		if re.MatchString("TestPythonWireFuzzVerdictPipe") {
			t.Error("wire-fuzz omnibus remains in a package shard")
		}
		for name := range focused {
			if re.MatchString(name) {
				focused[name] = true
			}
		}
	}
	for name, count := range want {
		if count != 1 {
			t.Errorf("wire-fuzz row %s appears %d times", name, count)
		}
	}
	for name, found := range focused {
		if !found {
			t.Errorf("focused test %s missing from package shards", name)
		}
	}
}
