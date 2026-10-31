package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryArrayRoot)
}

func TestLuauCarriesJSONArrayRoot(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, `package probe

table Goal
{
    x int32
}

table GoalList | json = "array"
{
    goals []Goal
}
`)
	for _, target := range c.Targets() {
		_, err := c.Generate(u, target, Options{"runtime": "standalone"})
		if target == "luau" {
			if err != nil {
				t.Errorf("Luau refused json array root: %v", err)
			}
			continue
		}
		if target == "go" {
			if err != nil {
				t.Errorf("Go oracle refused json array root: %v", err)
			}
			continue
		}
		if err != nil && !strings.Contains(err.Error(), `json = "array" on table GoalList`) {
			t.Errorf("target %q error = %v, want the named array-root refusal", target, err)
		}
	}
}
