package ci

import (
	"encoding/json"
	"regexp"
	"strconv"
	"testing"
)

// The delegated packet core must run once per shard, with other tests retained.
func TestPythonPacketCoreCIPlan(t *testing.T) {
	out := pythonCIPlan(t)
	var rows []struct {
		Target  string `json:"target"`
		Package string `json:"package"`
		Test    string `json:"test"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	targets := map[string]int{}
	auto := map[string]int{}
	names := []string{"TestPythonPacketCore", "TestPythonPacketCoreShardPlan", "TestPythonPacketLayoutNegativeControl", "TestPythonNameListsAreThePins"}
	for _, row := range rows {
		targets[row.Target]++
		if row.Package != "github.com/mas-bandwidth/schema/v3/internal/codegen/python" || row.Target != "" {
			continue
		}
		re, err := regexp.Compile("^" + row.Test + "$")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if re.MatchString(name) {
				auto[name]++
			}
		}
	}
	for i := range 8 {
		target := "python-ci-packet-core-" + strconv.Itoa(i)
		if targets[target] != 1 {
			t.Errorf("%s appears %d times", target, targets[target])
		}
	}
	if targets["python-ci-packet-core-coverage"] != 1 {
		t.Errorf("coverage support appears %d times", targets["python-ci-packet-core-coverage"])
	}
	for _, name := range names {
		want := 1
		if name == "TestPythonPacketCore" {
			want = 0
		}
		if auto[name] != want {
			t.Errorf("%s automatically selected %d times, want %d", name, auto[name], want)
		}
	}
}
