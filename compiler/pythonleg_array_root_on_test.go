package compiler

import "testing"

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryArrayRoot) }

func TestPythonArrayRootPublicDriver(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package arrayprobe\ntable Numbers | json=\"array\" { values []int32 }\n")
	if _, err := New().Generate(u, "python", nil); err != nil {
		t.Fatalf("Python refuses array root: %v", err)
	}
}
