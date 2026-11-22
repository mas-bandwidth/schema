package ci

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// The 96 real support rows (twelve interpreter, mode and arm streams, eight
// shards each) replace only the expensive omnibus; focused pipe and planted
// control tests must remain in the discovered package shards.
func TestPythonCIWireFuzzStandalonePartition(t *testing.T) {
	slowtest.Gate(t, "Python wire-fuzz CI plan")
	out := pythonCIPlan(t)
	var rows []struct {
		Target, Package, Test string
		Corpus                bool
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	for _, pin := range []string{"current", "floor"} {
		for _, mode := range []string{"normal", "dev"} {
			for _, arm := range []string{"plain", "mixed", "retain"} {
				for shard := range 8 {
					want["python-wirefuzz-"+pin+"-"+mode+"-"+arm+"-shard-"+strconv.Itoa(shard)] = 0
				}
			}
		}
	}
	focused := map[string]bool{"TestPythonWireFuzzFileDispatch": false, "TestPythonWireFuzzVerdictPipeNegativeControl": false}
	for _, row := range rows {
		if _, ok := want[row.Target]; ok {
			want[row.Target]++
			if !row.Corpus {
				t.Errorf("wire-fuzz row lacks corpus: %s", row.Target)
			}
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
